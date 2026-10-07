// Package traefik implements the Axiom Traefik network adapter (#84): it writes
// Axiom-owned Traefik *file-provider* dynamic configuration for a deployment,
// so a managed container becomes reachable at a hostname without ever touching
// unrelated Traefik configuration.
//
// Design constraints (issue #84, docs/architecture/agent-protocol.md §6):
//
//   - Bounded, Axiom-owned files only: every file this package writes is named
//     axiom-<deploymentID>.yml. Reconcile and Remove only ever look at that
//     pattern, and Reconcile additionally parses the filename as a deployment
//     id — any other file in the dynamic directory (including an axiom-*.yml
//     that is not a valid deployment id) is left untouched.
//   - Validated inputs: the domain is a lowercase hostname with no scheme or
//     port (ValidDomain), the container name follows the Axiom naming rules,
//     and the container is Axiom-managed within the deployment scope
//     (ownership.AssertContainer, or an injected ContainerVerifier that does
//     the live Docker inspection).
//   - Explicit TLS: when Request.TLS is true the config emits a web router that
//     redirects to a websecure router terminating TLS with the ACME resolver
//     "le"; when false only a plain web router is emitted.
//   - Atomic writes: config is rendered to a temp file in the same directory,
//     fsynced, chmod'd 0644 and renamed into place. A render or write failure
//     never leaves a partial file behind.
//
// The *static* Traefik configuration that defines the web/websecure
// entrypoints and the ACME resolver named "le" is operator-managed and lives
// outside this file; this package only owns the per-deployment dynamic file.
package traefik

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
)

// Stable typed errors. ErrNotManaged is the ownership sentinel so callers can
// match it with errors.Is (as the runtime/docker adapter does).
var (
	// ErrInvalidDomain means the requested domain is not a bare lowercase
	// hostname (it carries a scheme, a port, uppercase, whitespace or an
	// invalid label).
	ErrInvalidDomain = errors.New("traefik: invalid domain")
	// ErrNotManaged means the target container is not Axiom-managed within the
	// deployment scope; the adapter never routes to it.
	ErrNotManaged = ownership.ErrNotManaged
	// ErrInvalidDeployment means the deployment id is not dep_<24 hex>.
	ErrInvalidDeployment = errors.New("traefik: invalid deployment id")
	// ErrInvalidPort means the container port is outside 1-65535.
	ErrInvalidPort = errors.New("traefik: invalid port")
	// ErrNoDynamicDir means the adapter has no dynamic configuration directory
	// configured.
	ErrNoDynamicDir = errors.New("traefik: dynamic directory not configured")
)

const (
	filePrefix   = "axiom-"
	fileSuffix   = ".yml"
	fileMode     = 0o644
	dirMode      = 0o755
	certResolver = "le"
)

var deploymentRe = regexp.MustCompile(`^dep_[0-9a-f]{24}$`)

// ValidDomain reports whether domain is a bare lowercase DNS hostname: one or
// more labels of [a-z0-9] with internal hyphens, separated by dots, no scheme,
// no port, no uppercase, no whitespace and no wildcard. This is deliberately
// stricter than the protocol's payload check because the value is emitted into
// the router rule.
func ValidDomain(domain string) bool {
	if domain == "" || len(domain) > 253 {
		return false
	}
	if strings.ContainsAny(domain, " \t\r\n/:*@?#%") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if !validLabel(label) {
			return false
		}
	}
	return true
}

func validLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == len(label)-1 {
				return false // no leading/trailing hyphen
			}
		default:
			return false
		}
	}
	return true
}

// Request is one NETWORK configuration: expose Container:Port at Domain, with
// TLS when requested.
type Request struct {
	Container    string
	Domain       string
	Port         int
	TLS          bool
	DeploymentID string
	ServerID     string
	// Labels are the target container's ownership labels, used to enforce
	// ownership.AssertContainer. The dispatcher obtains them from
	// runtime/docker.Adapter.Inspect. When Verify is wired, Labels may be nil.
	Labels map[string]string
}

// ContainerVerifier enforces the ownership boundary for the target container.
// The dispatcher wires an implementation backed by runtime/docker (Inspect →
// ownership.AssertContainer); tests inject a fake. It must return an error
// matching ownership.ErrNotManaged for a foreign container.
type ContainerVerifier interface {
	VerifyContainer(ctx context.Context, container, deploymentID string) error
}

// ContainerVerifierFunc adapts a function to ContainerVerifier.
type ContainerVerifierFunc func(ctx context.Context, container, deploymentID string) error

// VerifyContainer implements ContainerVerifier.
func (f ContainerVerifierFunc) VerifyContainer(ctx context.Context, container, deploymentID string) error {
	return f(ctx, container, deploymentID)
}

// Adapter writes per-deployment Traefik file-provider configuration.
type Adapter struct {
	// DynamicDir is the Traefik file-provider directory (e.g.
	// /etc/traefik/dynamic). Required for Configure/Reconcile/Remove.
	DynamicDir string
	// Verify enforces container ownership. When nil, Configure falls back to
	// ownership.AssertContainer with Request.Labels (fail-closed: no labels
	// means ErrNotManaged).
	Verify ContainerVerifier
	// Marshal renders the dynamic config. Nil = the built-in renderer; tests
	// inject a failing renderer to exercise atomicity.
	Marshal func(Request) ([]byte, error)
}

// Configure validates the request, enforces container ownership and atomically
// writes axiom-<deploymentID>.yml. Re-configuring the same deployment is
// idempotent (the file is rewritten with identical content).
func (a *Adapter) Configure(ctx context.Context, req Request) error {
	if !deploymentRe.MatchString(req.DeploymentID) {
		return fmt.Errorf("%w: %q", ErrInvalidDeployment, req.DeploymentID)
	}
	if !ValidDomain(req.Domain) {
		return fmt.Errorf("%w: %q", ErrInvalidDomain, req.Domain)
	}
	if req.Port < 1 || req.Port > 65535 {
		return fmt.Errorf("%w: %d", ErrInvalidPort, req.Port)
	}
	if err := a.verifyContainer(ctx, req); err != nil {
		return err
	}
	if a.DynamicDir == "" {
		return ErrNoDynamicDir
	}
	data, err := a.render(req)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.DynamicDir, dirMode); err != nil {
		return fmt.Errorf("traefik: create dynamic directory: %w", err)
	}
	return writeAtomic(a.DynamicDir, fileName(req.DeploymentID), data, fileMode)
}

// Remove deletes the deployment's dynamic file. It is idempotent: removing a
// deployment with no file succeeds. Only the exact axiom-<deploymentID>.yml
// path is ever removed.
func (a *Adapter) Remove(deploymentID string) error {
	if !deploymentRe.MatchString(deploymentID) {
		return fmt.Errorf("%w: %q", ErrInvalidDeployment, deploymentID)
	}
	if a.DynamicDir == "" {
		return ErrNoDynamicDir
	}
	err := os.Remove(filepath.Join(a.DynamicDir, fileName(deploymentID)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Reconcile removes every axiom-<deploymentID>.yml whose deployment is not in
// active, and returns the removed file names. It never touches a file that is
// not an Axiom-owned dynamic config: foreign names are skipped, and so is an
// axiom-*.yml whose name does not parse as a deployment id.
func (a *Adapter) Reconcile(active []string) ([]string, error) {
	if a.DynamicDir == "" {
		return nil, ErrNoDynamicDir
	}
	entries, err := os.ReadDir(a.DynamicDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	keep := make(map[string]bool, len(active))
	for _, dep := range active {
		keep[dep] = true
	}
	var removed []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		dep, ok := parseFileName(name)
		if !ok {
			continue // never touch non-axiom (or unparseable) files
		}
		if keep[dep] {
			continue
		}
		if err := os.Remove(filepath.Join(a.DynamicDir, name)); err != nil {
			return removed, fmt.Errorf("traefik: remove %s: %w", name, err)
		}
		removed = append(removed, name)
	}
	sort.Strings(removed)
	return removed, nil
}

// verifyContainer applies the ownership boundary: name rules first, then the
// injected verifier, or ownership.AssertContainer with the supplied labels.
func (a *Adapter) verifyContainer(ctx context.Context, req Request) error {
	if !ownership.ValidateName(req.Container) {
		return fmt.Errorf("%w: %q", ownership.ErrInvalidName, req.Container)
	}
	if a.Verify != nil {
		return a.Verify.VerifyContainer(ctx, req.Container, req.DeploymentID)
	}
	return ownership.AssertContainer(req.Container, req.Labels, req.DeploymentID)
}

func (a *Adapter) render(req Request) ([]byte, error) {
	if a.Marshal != nil {
		return a.Marshal(req)
	}
	return render(req), nil
}

// render produces the deterministic dynamic configuration document. Every
// interpolated value is validated upstream, so no quoting is required beyond
// the router rule and service URL literals.
func render(req Request) []byte {
	base := filePrefix + req.DeploymentID
	var b strings.Builder
	b.WriteString("# Generated by Axiom; do not edit. Deployment " + req.DeploymentID + ".\n")
	b.WriteString("# Static Traefik config (entryPoints web/websecure and the ACME resolver\n")
	b.WriteString("# named \"" + certResolver + "\") is operator-managed and lives outside this file.\n")
	b.WriteString("http:\n")
	b.WriteString("  routers:\n")
	b.WriteString("    " + base + "-web:\n")
	b.WriteString("      rule: \"Host(`" + req.Domain + "`)\"\n")
	b.WriteString("      entryPoints:\n")
	b.WriteString("        - web\n")
	if req.TLS {
		b.WriteString("      middlewares:\n")
		b.WriteString("        - " + base + "-redirect\n")
	}
	b.WriteString("      service: " + base + "\n")
	if req.TLS {
		b.WriteString("    " + base + "-websecure:\n")
		b.WriteString("      rule: \"Host(`" + req.Domain + "`)\"\n")
		b.WriteString("      entryPoints:\n")
		b.WriteString("        - websecure\n")
		b.WriteString("      service: " + base + "\n")
		b.WriteString("      tls:\n")
		b.WriteString("        certResolver: " + certResolver + "\n")
		b.WriteString("  middlewares:\n")
		b.WriteString("    " + base + "-redirect:\n")
		b.WriteString("      redirectScheme:\n")
		b.WriteString("        scheme: https\n")
		b.WriteString("        permanent: true\n")
	}
	b.WriteString("  services:\n")
	b.WriteString("    " + base + ":\n")
	b.WriteString("      loadBalancer:\n")
	b.WriteString("        servers:\n")
	b.WriteString("          - url: \"http://" + req.Container + ":" + strconv.Itoa(req.Port) + "\"\n")
	return []byte(b.String())
}

// fileName returns the canonical Axiom dynamic-config file name.
func fileName(deploymentID string) string {
	return filePrefix + deploymentID + fileSuffix
}

// parseFileName extracts the deployment id from an axiom-<deploymentID>.yml
// name. It reports false for anything else.
func parseFileName(name string) (string, bool) {
	if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
		return "", false
	}
	dep := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), fileSuffix)
	if !deploymentRe.MatchString(dep) {
		return "", false
	}
	return dep, true
}

// writeAtomic writes data to dir/name via a temp file in the same directory,
// fsync, chmod and rename. The temp file is always removed on failure, so no
// partial config is ever visible.
func writeAtomic(dir, name string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return fmt.Errorf("traefik: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("traefik: write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("traefik: chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("traefik: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("traefik: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("traefik: rename temp file: %w", err)
	}
	cleanup = false
	return nil
}
