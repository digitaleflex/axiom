// Package recovery implements Agent startup reconciliation and recovery
// classification (#82): what happens after a process restart, a network
// interruption or a partial deployment failure.
//
// Two independent entry points:
//
//   - LoadInterrupted classifies the operations that were cut off by the
//     restart. It is a pure read over the state store (no side effects) and
//     returns a deterministic plan: idempotent operations (VERIFY) are
//     resumable, everything else needs reconciliation against the runtime.
//
//   - Reconciler.Reconcile compares the Axiom-managed containers that actually
//     exist on the host with the operations the agent has a durable record of.
//     It never modifies anything it does not own:
//
//   - unmanaged containers (no Axiom ownership labels) are ignored entirely;
//
//   - managed containers whose deployment the Engine still manages are checked
//     against local state (a runtime with no terminal state record is reported
//     as "runtime exists, local state missing" and is never adopted blindly);
//
//   - managed containers whose deployment the Engine no longer manages are
//     reported as orphans. They are removed only when AllowCleanup is set AND
//     the deployment is explicitly listed in RemoveDeployments, and even then
//     ownership is re-verified through Inspect immediately before removal.
//
// Reconcile produces a Report; the agent main loop relays it to the Engine
// (the wire message is out of scope here — see docs/architecture/
// agent-protocol.md §11: the agent reports current state after reconnect).
package recovery

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
	"github.com/digitaleflex/axiom/services/agent/internal/state"
)

// ContainerInfo is the reconciler's view of one host container. Labels are the
// only authority on ownership; Name is used for inspection/removal.
type ContainerInfo struct {
	Name   string
	Labels map[string]string
}

// Runtime is the minimal read/mutate surface reconciliation needs. The Docker
// adapter (#83) is wrapped into it by the agent wiring; tests fake it. List
// returns every Axiom-managed container (the implementation filters on the
// ownership label); Inspect returns one container; Remove deletes one.
type Runtime interface {
	List() ([]ContainerInfo, error)
	Inspect(name string) (ContainerInfo, error)
	Remove(name string) error
}

// Store is the read surface of the durable operation state store
// (services/agent/internal/state, #81).
type Store interface {
	// ListByDeployment returns every recorded entry for a deployment.
	ListByDeployment(deploymentID string) []state.Entry
	// ListInterrupted returns every interrupted or still-active entry.
	ListInterrupted() []state.Entry
}

// Reconciler performs startup reconciliation. It is safe for concurrent use
// only if Runtime and Store are.
type Reconciler struct {
	Runtime Runtime
	State   Store
	Log     *slog.Logger

	// AllowCleanup enables removal of orphan containers. When false (the
	// default) orphans are only reported, never touched.
	AllowCleanup bool
	// RemoveDeployments is the explicit allow-list of deployments whose orphan
	// containers may be removed. Removal requires AllowCleanup AND membership
	// here; a deployment absent from the list is only reported.
	RemoveDeployments []string
}

// Finding is one reconciliation observation.
type Finding struct {
	Container    string
	DeploymentID string
	// Reason is a short human explanation of the classification.
	Reason string
	// Removed is true when this finding's container was removed.
	Removed bool
}

// Report is the deterministic outcome of one Reconcile pass.
type Report struct {
	// Scanned is the number of containers returned by Runtime.List.
	Scanned int
	// Managed is the number of Axiom-managed containers among Scanned.
	Managed int
	// Ignored is the number of unmanaged containers, never modified.
	Ignored int
	// Consistent are managed containers whose deployment is still managed and
	// which have a terminal state record: no action.
	Consistent []Finding
	// MissingState are managed containers whose deployment is still managed but
	// which have no terminal state record ("runtime exists, local state
	// missing"): reported, never adopted.
	MissingState []Finding
	// Orphans are managed containers whose deployment is no longer managed:
	// reported; removed only under the AllowCleanup allow-list.
	Orphans []Finding
	// Removed are the orphans that were actually removed.
	Removed []Finding
}

// Reconcile compares host containers against managedDeployments (the
// deployments the Engine still manages for this agent) and the local state
// store. It returns a deterministic Report. Unmanaged containers are never
// modified.
func (r *Reconciler) Reconcile(ctx context.Context, managedDeployments []string) (Report, error) {
	var report Report
	if r.Runtime == nil {
		return report, fmt.Errorf("recovery: runtime is required")
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}

	managed := make(map[string]bool, len(managedDeployments))
	for _, d := range managedDeployments {
		managed[d] = true
	}
	removable := make(map[string]bool, len(r.RemoveDeployments))
	for _, d := range r.RemoveDeployments {
		removable[d] = true
	}

	infos, err := r.Runtime.List()
	if err != nil {
		return report, fmt.Errorf("recovery: list containers: %w", err)
	}
	report.Scanned = len(infos)

	// Deterministic processing order.
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })

	for _, info := range infos {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if !ownership.IsManaged(info.Labels) {
			// Never touch a container Axiom does not own.
			report.Ignored++
			continue
		}
		report.Managed++
		dep := info.Labels[ownership.LabelDeployment]

		if !managed[dep] {
			f := Finding{
				Container:    info.Name,
				DeploymentID: dep,
				Reason:       "deployment is not managed by this agent (orphan)",
			}
			if r.AllowCleanup && removable[dep] {
				if err := r.removeOrphan(info, dep); err != nil {
					return report, err
				}
				f.Removed = true
				report.Removed = append(report.Removed, f)
			}
			report.Orphans = append(report.Orphans, f)
			continue
		}

		if r.hasTerminalState(dep) {
			report.Consistent = append(report.Consistent, Finding{
				Container:    info.Name,
				DeploymentID: dep,
				Reason:       "runtime and local state agree",
			})
			continue
		}
		report.MissingState = append(report.MissingState, Finding{
			Container:    info.Name,
			DeploymentID: dep,
			Reason:       "runtime exists, local state missing",
		})
	}
	return report, nil
}

// hasTerminalState reports whether the store holds a terminal record for the
// deployment. "Terminal" includes INTERRUPTED (the operation did reach a
// recorded stopping point); interrupted work is surfaced separately by
// LoadInterrupted. A nil store means no local state at all.
func (r *Reconciler) hasTerminalState(deploymentID string) bool {
	if r.State == nil {
		return false
	}
	for _, e := range r.State.ListByDeployment(deploymentID) {
		if e.Phase.Terminal() {
			return true
		}
	}
	return false
}

// removeOrphan re-verifies ownership through Inspect immediately before
// removal (defense in depth) and refuses anything that no longer matches.
func (r *Reconciler) removeOrphan(info ContainerInfo, dep string) error {
	cur, err := r.Runtime.Inspect(info.Name)
	if err != nil {
		return fmt.Errorf("recovery: inspect orphan %s: %w", info.Name, err)
	}
	if !ownership.IsManaged(cur.Labels) || cur.Labels[ownership.LabelDeployment] != dep {
		return fmt.Errorf("recovery: refusing to remove %s: ownership changed", info.Name)
	}
	if err := r.Runtime.Remove(info.Name); err != nil {
		return fmt.Errorf("recovery: remove orphan %s: %w", info.Name, err)
	}
	if r.Log != nil {
		r.Log.Info("recovery: removed orphan container",
			"container", info.Name, "deploymentId", dep)
	}
	return nil
}

// Plan is the classification of operations interrupted by a restart.
type Plan struct {
	// Resumable are interrupted operations that are idempotent by design
	// (VERIFY) and may be re-executed safely.
	Resumable []state.Entry
	// NeedsReconciliation are interrupted operations that may have applied
	// partial effects and must be checked against the runtime before any
	// further action.
	NeedsReconciliation []state.Entry
}

// LoadInterrupted classifies the store's interrupted operations. It performs
// reads only and returns a deterministically ordered plan (by StartedAt, then
// OperationID).
func LoadInterrupted(st Store) Plan {
	var plan Plan
	if st == nil {
		return plan
	}
	entries := st.ListInterrupted()
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].StartedAt.Equal(entries[j].StartedAt) {
			return entries[i].StartedAt.Before(entries[j].StartedAt)
		}
		return entries[i].OperationID < entries[j].OperationID
	})
	for _, e := range entries {
		if e.Resumable || state.ResumableOperation(e.Type) {
			plan.Resumable = append(plan.Resumable, e)
			continue
		}
		plan.NeedsReconciliation = append(plan.NeedsReconciliation, e)
	}
	return plan
}
