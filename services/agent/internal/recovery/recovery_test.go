package recovery_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/recovery"
	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
	"github.com/digitaleflex/axiom/services/agent/internal/state"
)

const (
	depA = "dep_aaaaaaaaaaaaaaaaaaaaaaaa"
	depB = "dep_bbbbbbbbbbbbbbbbbbbbbbbb"
)

func managedLabels(dep string) map[string]string {
	return map[string]string{
		ownership.LabelManaged:    ownership.ManagedTrue,
		ownership.LabelDeployment: dep,
	}
}

func unmanagedLabels() map[string]string {
	return map[string]string{"com.example.role": "web"}
}

type fakeRuntime struct {
	infos      []recovery.ContainerInfo
	byName     map[string]recovery.ContainerInfo
	removed    []string
	inspectErr error
}

func newFakeRuntime(infos ...recovery.ContainerInfo) *fakeRuntime {
	f := &fakeRuntime{byName: map[string]recovery.ContainerInfo{}}
	for _, i := range infos {
		f.infos = append(f.infos, i)
		f.byName[i.Name] = i
	}
	return f
}

func (f *fakeRuntime) List() ([]recovery.ContainerInfo, error) {
	out := make([]recovery.ContainerInfo, len(f.infos))
	copy(out, f.infos)
	return out, nil
}

func (f *fakeRuntime) Inspect(name string) (recovery.ContainerInfo, error) {
	if f.inspectErr != nil {
		return recovery.ContainerInfo{}, f.inspectErr
	}
	i, ok := f.byName[name]
	if !ok {
		return recovery.ContainerInfo{}, errors.New("not found")
	}
	return i, nil
}

func (f *fakeRuntime) Remove(name string) error {
	f.removed = append(f.removed, name)
	delete(f.byName, name)
	return nil
}

type fakeStore struct {
	byDep       map[string][]state.Entry
	interrupted []state.Entry
}

func (f *fakeStore) ListByDeployment(d string) []state.Entry { return f.byDep[d] }
func (f *fakeStore) ListInterrupted() []state.Entry          { return f.interrupted }

func terminalEntry(dep, typ string, phase state.Phase) state.Entry {
	return state.Entry{OperationID: "op_" + dep + "_" + typ + "_1", DeploymentID: dep, Type: typ, Phase: phase}
}

func TestReconcileManagedKnownNoAction(t *testing.T) {
	rt := newFakeRuntime(recovery.ContainerInfo{Name: "axiom-web-1", Labels: managedLabels(depA)})
	st := &fakeStore{byDep: map[string][]state.Entry{depA: {terminalEntry(depA, protocol.OpCreateRuntime, state.PhaseCompleted)}}}
	r := &recovery.Reconciler{Runtime: rt, State: st}

	rep, err := r.Reconcile(context.Background(), []string{depA})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Managed != 1 || len(rep.Consistent) != 1 || len(rep.MissingState) != 0 || len(rep.Orphans) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if len(rt.removed) != 0 {
		t.Fatalf("removed = %v, want none", rt.removed)
	}
}

func TestReconcileMissingState(t *testing.T) {
	rt := newFakeRuntime(recovery.ContainerInfo{Name: "axiom-web-1", Labels: managedLabels(depA)})
	st := &fakeStore{byDep: map[string][]state.Entry{}}
	r := &recovery.Reconciler{Runtime: rt, State: st}

	rep, err := r.Reconcile(context.Background(), []string{depA})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.MissingState) != 1 || rep.MissingState[0].Container != "axiom-web-1" {
		t.Fatalf("MissingState = %+v", rep.MissingState)
	}
	if len(rt.removed) != 0 {
		t.Fatalf("missing-state container must never be removed")
	}
}

func TestReconcileOrphanReportedNotRemoved(t *testing.T) {
	rt := newFakeRuntime(recovery.ContainerInfo{Name: "axiom-web-1", Labels: managedLabels(depB)})
	st := &fakeStore{byDep: map[string][]state.Entry{}}
	r := &recovery.Reconciler{Runtime: rt, State: st}

	rep, err := r.Reconcile(context.Background(), []string{depA})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Orphans) != 1 || len(rep.Removed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if len(rt.removed) != 0 {
		t.Fatalf("orphan removed without AllowCleanup: %v", rt.removed)
	}
}

func TestReconcileOrphanRemovedWhenAllowListed(t *testing.T) {
	rt := newFakeRuntime(recovery.ContainerInfo{Name: "axiom-web-1", Labels: managedLabels(depB)})
	st := &fakeStore{byDep: map[string][]state.Entry{}}
	r := &recovery.Reconciler{
		Runtime:           rt,
		State:             st,
		AllowCleanup:      true,
		RemoveDeployments: []string{depB},
	}

	rep, err := r.Reconcile(context.Background(), []string{depA})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Orphans) != 1 || !rep.Orphans[0].Removed {
		t.Fatalf("orphan not marked removed: %+v", rep.Orphans)
	}
	if len(rep.Removed) != 1 || len(rt.removed) != 1 || rt.removed[0] != "axiom-web-1" {
		t.Fatalf("removed = %v report.Removed=%+v", rt.removed, rep.Removed)
	}
}

func TestReconcileAllowCleanupWithoutListDoesNotRemove(t *testing.T) {
	rt := newFakeRuntime(recovery.ContainerInfo{Name: "axiom-web-1", Labels: managedLabels(depB)})
	r := &recovery.Reconciler{Runtime: rt, AllowCleanup: true}

	rep, err := r.Reconcile(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rt.removed) != 0 || len(rep.Removed) != 0 {
		t.Fatalf("removed without allow-list membership: %v", rt.removed)
	}
}

func TestReconcileUnmanagedUntouched(t *testing.T) {
	rt := newFakeRuntime(
		recovery.ContainerInfo{Name: "random-db", Labels: unmanagedLabels()},
		recovery.ContainerInfo{Name: "no-labels"},
	)
	r := &recovery.Reconciler{Runtime: rt, AllowCleanup: true, RemoveDeployments: []string{depB}}

	rep, err := r.Reconcile(context.Background(), []string{depA})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ignored != 2 || rep.Managed != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if len(rep.Orphans) != 0 || len(rep.MissingState) != 0 || len(rep.Consistent) != 0 {
		t.Fatalf("unmanaged container classified: %+v", rep)
	}
	if len(rt.removed) != 0 {
		t.Fatalf("unmanaged container removed: %v", rt.removed)
	}
}

func TestReconcileDeterministicOrder(t *testing.T) {
	rt := newFakeRuntime(
		recovery.ContainerInfo{Name: "axiom-c", Labels: managedLabels(depB)},
		recovery.ContainerInfo{Name: "axiom-a", Labels: managedLabels(depB)},
		recovery.ContainerInfo{Name: "axiom-b", Labels: managedLabels(depB)},
	)
	r := &recovery.Reconciler{Runtime: rt}

	rep, err := r.Reconcile(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Orphans) != 3 {
		t.Fatalf("orphans = %+v", rep.Orphans)
	}
	got := []string{rep.Orphans[0].Container, rep.Orphans[1].Container, rep.Orphans[2].Container}
	if strings.Join(got, ",") != "axiom-a,axiom-b,axiom-c" {
		t.Fatalf("orphan order = %v", got)
	}
}

func TestReconcileRefusesOwnershipChange(t *testing.T) {
	rt := newFakeRuntime(recovery.ContainerInfo{Name: "axiom-web-1", Labels: managedLabels(depB)})
	// Inspect now reports an unmanaged container: removal must be refused.
	rt.byName["axiom-web-1"] = recovery.ContainerInfo{Name: "axiom-web-1", Labels: unmanagedLabels()}
	r := &recovery.Reconciler{Runtime: rt, AllowCleanup: true, RemoveDeployments: []string{depB}}

	if _, err := r.Reconcile(context.Background(), nil); err == nil {
		t.Fatal("expected refusal when ownership changed")
	}
	if len(rt.removed) != 0 {
		t.Fatalf("removed despite ownership change: %v", rt.removed)
	}
}

func TestReconcileRequiresRuntime(t *testing.T) {
	r := &recovery.Reconciler{}
	if _, err := r.Reconcile(context.Background(), nil); err == nil {
		t.Fatal("expected error without runtime")
	}
}

func TestLoadInterruptedClassification(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	mk := func(id, typ string, at time.Time, resumable bool) state.Entry {
		return state.Entry{
			OperationID: id, DeploymentID: depA, Type: typ,
			Phase: state.PhaseInterrupted, Interrupted: true, Resumable: resumable,
			StartedAt: at,
		}
	}
	verify := mk("op_"+depA+"_VERIFY_1", protocol.OpVerifyHealth, t0.Add(2*time.Second), true)
	create := mk("op_"+depA+"_CREATE_RUNTIME_1", protocol.OpCreateRuntime, t0.Add(1*time.Second), false)
	st := &fakeStore{interrupted: []state.Entry{verify, create}}

	plan := recovery.LoadInterrupted(st)
	if len(plan.Resumable) != 1 || plan.Resumable[0].OperationID != verify.OperationID {
		t.Fatalf("Resumable = %+v", plan.Resumable)
	}
	if len(plan.NeedsReconciliation) != 1 || plan.NeedsReconciliation[0].OperationID != create.OperationID {
		t.Fatalf("NeedsReconciliation = %+v", plan.NeedsReconciliation)
	}
}

func TestLoadInterruptedDeterministicOrder(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	mk := func(id string, at time.Time) state.Entry {
		return state.Entry{OperationID: id, DeploymentID: depA, Type: protocol.OpCreateRuntime,
			Phase: state.PhaseInterrupted, Interrupted: true, StartedAt: at}
	}
	// Input out of order; both non-resumable so they land in one bucket.
	st := &fakeStore{interrupted: []state.Entry{
		mk("op_"+depA+"_START_2", t0.Add(3*time.Second)),
		mk("op_"+depA+"_START_1", t0.Add(1*time.Second)),
		mk("op_"+depA+"_START_3", t0.Add(2*time.Second)),
	}}
	plan := recovery.LoadInterrupted(st)
	got := []string{plan.NeedsReconciliation[0].OperationID, plan.NeedsReconciliation[1].OperationID, plan.NeedsReconciliation[2].OperationID}
	want := "op_" + depA + "_START_1,op_" + depA + "_START_3,op_" + depA + "_START_2"
	if strings.Join(got, ",") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}

func TestLoadInterruptedNilStore(t *testing.T) {
	plan := recovery.LoadInterrupted(nil)
	if len(plan.Resumable) != 0 || len(plan.NeedsReconciliation) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}
