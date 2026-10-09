// Package ownership defines the Axiom resource ownership boundary (#89):
// the canonical label set that marks a Docker resource as Axiom-managed,
// the naming rules for Axiom-created containers and networks, and the
// assertion helpers that gate every runtime mutation. The package is pure
// validation and naming — it never executes commands and never talks to
// Docker, so it is safe to call from any authorization path.
package ownership

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Canonical Axiom label keys applied to every resource the Agent creates.
// #83/#84 must use these exact strings when labelling containers, networks
// and configuration.
const (
	LabelManaged     = "axiom.managed"
	LabelDeployment  = "axiom.deployment"
	LabelApplication = "axiom.application"
	LabelServer      = "axiom.server"
	LabelCreated     = "axiom.created"
)

// ManagedTrue is the only value of LabelManaged that marks a resource as
// Axiom-managed.
const ManagedTrue = "true"

// Typed errors returned by the assertion helpers. ErrForeignResource is the
// base sentinel: any error for a resource the Agent must not touch matches
// it via errors.Is, while the wrapped variants say why it is foreign.
var (
	// ErrForeignResource means the resource is not owned by this Axiom
	// deployment scope and must not be modified or removed.
	ErrForeignResource = errors.New("ownership: resource does not belong to Axiom")
	// ErrNotManaged means the resource carries no Axiom-managed label set.
	ErrNotManaged = fmt.Errorf("%w: not labelled %s=%s", ErrForeignResource, LabelManaged, ManagedTrue)
	// ErrWrongDeployment means the resource is Axiom-managed but belongs to
	// a different deployment than the operation's scope.
	ErrWrongDeployment = fmt.Errorf("%w: resource belongs to a different deployment", ErrForeignResource)
	// ErrWrongApplication means the resource is Axiom-managed but belongs to
	// a different application than the operation's scope (#145). Deployment
	// and application are distinct identities: matching deployment alone is
	// never sufficient to authorize a mutation.
	ErrWrongApplication = fmt.Errorf("%w: resource belongs to a different application", ErrForeignResource)
	// ErrInvalidName means a resource name violates the Axiom naming rules.
	ErrInvalidName = errors.New("ownership: invalid resource name")
)

// nameRe matches the Engine's server-name rules (services/engine/internal/
// server/registration.go): 1-63 chars, lowercase letters, digits, hyphens;
// must start and end with an alphanumeric.
var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateName reports whether name satisfies the Axiom resource naming
// rules — identical to the Engine's server names.
func ValidateName(name string) bool { return nameRe.MatchString(name) }

// ContainerName returns the canonical container name for an application
// slug within a deployment: axiom-<slug>-<shortid>. The slug must be a
// DNS-label-safe slug (lowercase letters, digits, hyphens); the final name
// is validated with ValidateName, so oversized combinations are rejected.
func ContainerName(appSlug, deploymentSuffix string) (string, error) {
	if !nameRe.MatchString(appSlug) {
		return "", fmt.Errorf("%w: slug %q", ErrInvalidName, appSlug)
	}
	name := "axiom-" + appSlug + "-" + deploymentSuffix
	if !ValidateName(name) {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return name, nil
}

// NetworkName returns the canonical network name for a server:
// axiom-<server>-net.
func NetworkName(serverID string) string {
	return "axiom-" + serverID + "-net"
}

// IsManaged reports whether labels mark the resource as Axiom-managed:
// axiom.managed must be exactly "true" AND both axiom.deployment and
// axiom.application must be present. Anything else — including a bare
// deployment label, or a resource stamped before the agent could tell an
// application from a deployment (#145) — is foreign.
func IsManaged(labels map[string]string) bool {
	return labels[LabelManaged] == ManagedTrue &&
		labels[LabelDeployment] != "" &&
		labels[LabelApplication] != ""
}

// NewLabels returns the canonical label set for a resource created for the
// given deployment/application/server, stamped with the current time.
func NewLabels(deploymentID, applicationID, serverID string) map[string]string {
	return map[string]string{
		LabelManaged:     ManagedTrue,
		LabelDeployment:  deploymentID,
		LabelApplication: applicationID,
		LabelServer:      serverID,
		LabelCreated:     time.Now().UTC().Format(time.RFC3339),
	}
}

// Scope is the resource identity an operation is authorized against (#145).
// Application and deployment are both mandatory and independent: nothing in
// this package ever derives one from the other.
type Scope struct {
	ApplicationID string
	DeploymentID  string
}

// AssertContainer verifies that the container with the given name and
// labels may be touched within the given scope: the name must satisfy the
// Axiom naming rules, the container must be Axiom-managed, and both its
// application and its deployment label must match the scope. A container of
// another application of the same deployment is refused with
// ErrWrongApplication.
func AssertContainer(name string, labels map[string]string, scope Scope) error {
	if !ValidateName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return assertScope(name, labels, scope)
}

// AssertNetwork verifies that the network with the given name and labels
// may be touched within the given scope: the name must follow the Axiom
// network pattern, the network must be Axiom-managed, and both its
// application and its deployment label must match the scope.
func AssertNetwork(name string, labels map[string]string, scope Scope) error {
	if !strings.HasPrefix(name, "axiom-") || !strings.HasSuffix(name, "-net") {
		return fmt.Errorf("%w: %q is not an Axiom network name", ErrInvalidName, name)
	}
	return assertScope(name, labels, scope)
}

// assertScope enforces the shared managed-and-in-scope check.
func assertScope(name string, labels map[string]string, scope Scope) error {
	if !IsManaged(labels) {
		return fmt.Errorf("%w: %q", ErrNotManaged, name)
	}
	if labels[LabelApplication] != scope.ApplicationID {
		return fmt.Errorf("%w: %q (scope application %s)", ErrWrongApplication, name, scope.ApplicationID)
	}
	if labels[LabelDeployment] != scope.DeploymentID {
		return fmt.Errorf("%w: %q (scope deployment %s)", ErrWrongDeployment, name, scope.DeploymentID)
	}
	return nil
}

// CleanupScope bounds what a cleanup pass may remove: only Axiom-managed
// resources whose application and deployment labels match the scope.
type CleanupScope struct {
	ApplicationID string
	DeploymentID  string
}

// MayRemove reports whether the named resource with these labels may be
// removed within the scope.
func (s CleanupScope) MayRemove(name string, labels map[string]string) bool {
	return IsManaged(labels) &&
		labels[LabelApplication] == s.ApplicationID &&
		labels[LabelDeployment] == s.DeploymentID
}

// Resource is a named, labelled resource (container or network) that is a
// candidate for cleanup.
type Resource struct {
	Name   string
	Labels map[string]string
}

// Removable returns the subset of resources that may be removed within the
// scope, preserving input order.
func (s CleanupScope) Removable(resources []Resource) []Resource {
	var out []Resource
	for _, r := range resources {
		if s.MayRemove(r.Name, r.Labels) {
			out = append(out, r)
		}
	}
	return out
}
