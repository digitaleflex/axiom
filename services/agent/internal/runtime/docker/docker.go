// Package docker implements the Axiom Docker runtime adapter (#83).
//
// Design constraints (issue #83, docs/architecture/agent-protocol.md §12):
//
//   - STDLIB ONLY: the agent module has no dependencies, so the Docker CLI is
//     driven through os/exec with FIXED argv — never a shell, never string
//     concatenation of untrusted input (same pattern as
//     services/engine/internal/build.ExecBuilder).
//   - Ownership boundary: every mutation (start/stop/remove/cleanup) verifies
//     the target container exists AND is Axiom-managed (ownership.IsManaged)
//     before touching it; anything else is refused with ErrNotManaged.
//   - Environment injection comes ONLY from an explicit map[string]string on
//     the create spec — never os.Environ or any host state.
//
// Labels, naming and the managed guard come from the canonical ownership
// package (services/agent/internal/security/ownership, #89). Health probing
// (#85) and log shipping (#86) build on this adapter; they are intentionally
// out of scope here.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
)

// Stable machine-readable error codes reported to the Engine.
const (
	CodeInvalidInput      = "RUNTIME_INVALID_INPUT"
	CodeNotManaged        = "RUNTIME_NOT_MANAGED"
	CodeImageMissing      = "RUNTIME_IMAGE_MISSING"
	CodeContainerNotFound = "RUNTIME_CONTAINER_NOT_FOUND"
	CodeDockerFailed      = "RUNTIME_DOCKER_FAILED"
	CodeInterrupted       = "RUNTIME_INTERRUPTED"
)

// ErrNotManaged is the ownership package's sentinel: the target exists but is
// not Axiom-managed. The adapter never touches such containers.
var ErrNotManaged = ownership.ErrNotManaged

// Error is the normalized failure type for the Engine.
type Error struct {
	Code     string
	Message  string
	Cause    error
	ExitCode int    // docker exit code when known; -1 for spawn failures
	Log      string // bounded docker output
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("runtime: %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("runtime: %s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// ErrorCode implements the dispatcher's ErrorCoder contract so this adapter's
// stable code (RUNTIME_IMAGE_MISSING, RUNTIME_NOT_MANAGED, …) surfaces verbatim
// in Result.ErrorCode instead of being degraded to INTERNAL by classifyError.
func (e *Error) ErrorCode() string { return e.Code }

// IsNotManaged reports whether err was caused by ErrNotManaged.
func IsNotManaged(err error) bool {
	return errors.Is(err, ErrNotManaged)
}

// Runner executes a fixed argv command. exit is the process exit code (-1 for
// spawn failures); err is non-nil when the command could not be started, was
// interrupted, or exited non-zero (as *exec.ExitError). Callers treat exit as
// the authoritative command outcome and err for spawn/interrupt handling.
type Runner interface {
	Run(ctx context.Context, argv ...string) (stdout string, exit int, err error)
}

// ExecRunner is the production Runner: it invokes the Docker CLI via
// os/exec.CommandContext (no shell). Env nil = inherit parent environment.
type ExecRunner struct {
	Docker string
	Env    []string
}

func (r *ExecRunner) Run(ctx context.Context, argv ...string) (string, int, error) {
	bin := r.Docker
	if bin == "" {
		bin = "docker"
	}
	cmd := exec.CommandContext(ctx, bin, argv...)
	if r.Env != nil {
		cmd.Env = r.Env
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	if runErr == nil {
		return out.String(), 0, nil
	}
	if ee, ok := runErr.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode(), runErr
	}
	return out.String(), -1, runErr
}

// Adapter is the bounded Docker runtime adapter (#83).
type Adapter struct {
	// Docker is the docker CLI path; empty = "docker" from PATH.
	Docker string
	// Runner executes commands; nil = ExecRunner{Docker: Docker}.
	Runner Runner
	// Timeout bounds every docker invocation; 0 = no timeout.
	Timeout time.Duration
	// Now is the clock for duration measurement; nil = time.Now.
	Now func() time.Time
}

func (a *Adapter) runner() Runner {
	if a.Runner != nil {
		return a.Runner
	}
	return &ExecRunner{Docker: a.Docker}
}

func (a *Adapter) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Adapter) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if a.Timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, a.Timeout)
}

// Validation mirrors the protocol payload rules (services/agent/internal/
// protocol) defensively: the adapter re-checks everything even though the
// dispatcher (#80) validates first. Container names use the canonical Axiom
// naming rules (ownership.ValidateName) — stricter than the protocol's idRe,
// because every container the adapter creates or touches is Axiom-named.
var (
	deploymentRe  = regexp.MustCompile(`^dep_[0-9a-f]{24}$`)
	applicationRe = regexp.MustCompile(`^app_[0-9a-f]{24}$`)
	envKeyRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	sha256Re      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// validImageRef mirrors protocol.validImageRef: printable ASCII, no leading or
// trailing whitespace, bounded length; sha256 digests are accepted too.
func validImageRef(s string) bool {
	if sha256Re.MatchString(s) {
		return true
	}
	if len(s) == 0 || len(s) > 512 || len(s) != len(trimSpace(s)) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	for i < j && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

// ResourceLimits translates to docker --memory / --cpus. Zero = flag unset.
type ResourceLimits struct {
	MemoryMB int // docker --memory <n>m
	NanoCPUs int // docker --cpus <NanoCPUs/1e9>
}

// CreateSpec describes one container creation (CREATE_RUNTIME).
type CreateSpec struct {
	// DeploymentID and ApplicationID are distinct identities (#145) and both
	// are required: the deployment is one rollout attempt, the application is
	// what it rolls out. The adapter refuses a spec missing either, because
	// the ownership labels it would stamp would then be un-attributable.
	DeploymentID  string
	ApplicationID string
	ServerID      string
	Container     string // Axiom-assigned (ownership.ContainerName), DNS-safe
	ImageRef      string // image reference or sha256 digest
	Port          int    // container port 1-65535
	// Env is the ONLY source of container environment variables. It is never
	// augmented with os.Environ or any host state: the container environment
	// is exactly this explicit, caller-approved map, passed as -e KEY=VALUE
	// argv items (never concatenated into a shell string).
	Env map[string]string
	// Labels are merged with the canonical managed labels (which can never be
	// overridden through this map).
	Labels  map[string]string
	Limits  ResourceLimits
	Command []string // appended after the image: docker create ... image cmd...
}

func (s CreateSpec) validate() *Error {
	if !ownership.ValidateName(s.Container) {
		return &Error{Code: CodeInvalidInput, Message: "invalid container name"}
	}
	if !validImageRef(s.ImageRef) {
		return &Error{Code: CodeInvalidInput, Message: "invalid imageRef"}
	}
	if s.Port < 1 || s.Port > 65535 {
		return &Error{Code: CodeInvalidInput, Message: "port must be 1-65535"}
	}
	if !deploymentRe.MatchString(s.DeploymentID) {
		return &Error{Code: CodeInvalidInput, Message: "invalid deploymentId"}
	}
	if !applicationRe.MatchString(s.ApplicationID) {
		return &Error{Code: CodeInvalidInput, Message: "invalid applicationId"}
	}
	for k, v := range s.Env {
		if !envKeyRe.MatchString(k) {
			return &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("invalid env key %q", k)}
		}
		if strings.Contains(v, "\x00") {
			return &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("env value for %q contains NUL", k)}
		}
	}
	if s.Limits.MemoryMB < 0 || s.Limits.NanoCPUs < 0 {
		return &Error{Code: CodeInvalidInput, Message: "resource limits must not be negative"}
	}
	return nil
}

// PortMapping is one host binding for a container port.
type PortMapping struct {
	HostIP        string
	HostPort      int
	ContainerPort int
}

// ContainerInfo is the adapter's view of a container.
type ContainerInfo struct {
	Name       string
	Image      string
	ImageID    string // sha256:... image digest from inspect
	Running    bool
	Status     string // docker status: running, exited, ...
	Ports      []PortMapping
	Labels     map[string]string
	DurationMs int64 // wall time of the create call (0 when from inspect)
}

// EnsureImage makes imageRef available locally: `docker image inspect` first,
// `docker pull` only when missing (idempotent).
func (a *Adapter) EnsureImage(ctx context.Context, imageRef string) error {
	if !validImageRef(imageRef) {
		return &Error{Code: CodeInvalidInput, Message: "invalid imageRef"}
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	_, exit, err := a.runner().Run(ctx, "image", "inspect", imageRef)
	if err == nil && exit == 0 {
		return nil
	}
	if ctx.Err() != nil {
		return &Error{Code: CodeInterrupted, Message: "image inspect interrupted", Cause: ctx.Err()}
	}
	// Missing (or unreadable) image: pull it.
	out, exit, err := a.runner().Run(ctx, "pull", imageRef)
	if ctx.Err() != nil {
		return &Error{Code: CodeInterrupted, Message: "image pull interrupted", Cause: ctx.Err()}
	}
	if exit != 0 {
		return &Error{Code: CodeImageMissing, Message: fmt.Sprintf("docker pull exited with code %d", exit), ExitCode: exit, Log: out}
	}
	if err != nil {
		return &Error{Code: CodeDockerFailed, Message: "docker pull failed to start", Cause: err, Log: out}
	}
	return nil
}

// Create creates (does not start) the container. The image is ensured first
// (pull when missing). Idempotent: an existing managed container with the same
// image is returned as-is; an existing unmanaged container is refused with
// ErrNotManaged. The host port is bound ephemerally on 127.0.0.1
// (-p 127.0.0.1::<port>) and reported via inspect: until the container
// starts, docker reports the configured binding with HostPort 0 (the
// ephemeral port is allocated at start); after Start, the allocated port.
func (a *Adapter) Create(ctx context.Context, spec CreateSpec) (ContainerInfo, error) {
	if err := spec.validate(); err != nil {
		return ContainerInfo{}, err
	}
	if err := a.EnsureImage(ctx, spec.ImageRef); err != nil {
		return ContainerInfo{}, err
	}
	// Idempotency: never recreate an existing container.
	info, err := a.Inspect(ctx, spec.Container)
	if err == nil {
		if !ownership.IsManaged(info.Labels) {
			return ContainerInfo{}, &Error{Code: CodeNotManaged, Message: fmt.Sprintf("container %q exists and is not managed by axiom", spec.Container), Cause: ErrNotManaged}
		}
		if info.Image != spec.ImageRef && info.ImageID != spec.ImageRef {
			return ContainerInfo{}, &Error{Code: CodeInvalidInput, Message: fmt.Sprintf("container %q already exists with a different image", spec.Container)}
		}
		return info, nil
	}
	if !isNotFound(err) {
		return ContainerInfo{}, err
	}

	args := []string{"create", "--name", spec.Container}
	// Bind per payload on loopback with an ephemeral host port: a fixed
	// -p <hostPort>:<containerPort> would collide on a shared host.
	args = append(args, "-p", fmt.Sprintf("127.0.0.1::%d", spec.Port))
	if spec.Limits.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", spec.Limits.MemoryMB))
	}
	if spec.Limits.NanoCPUs > 0 {
		args = append(args, "--cpus", formatCPUs(spec.Limits.NanoCPUs))
	}
	for _, k := range sortedKeys(spec.Env) {
		args = append(args, "-e", k+"="+spec.Env[k])
	}
	for _, kv := range labelPairs(spec) {
		args = append(args, "--label", kv)
	}
	args = append(args, spec.ImageRef)
	args = append(args, spec.Command...)

	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	start := a.now()
	out, exit, err := a.runner().Run(ctx, args...)
	if ctx.Err() != nil {
		return ContainerInfo{}, &Error{Code: CodeInterrupted, Message: "container create interrupted", Cause: ctx.Err()}
	}
	if exit != 0 {
		return ContainerInfo{}, &Error{Code: CodeDockerFailed, Message: fmt.Sprintf("docker create exited with code %d", exit), ExitCode: exit, Log: out}
	}
	if err != nil {
		return ContainerInfo{}, &Error{Code: CodeDockerFailed, Message: "docker create failed to start", Cause: err, Log: out}
	}
	info, err = a.Inspect(ctx, spec.Container)
	if err != nil {
		return ContainerInfo{}, &Error{Code: CodeDockerFailed, Message: "container created but inspect failed", Cause: err}
	}
	info.DurationMs = a.now().Sub(start).Milliseconds()
	return info, nil
}

// Start starts a managed container (START). Refuses unknown or unmanaged
// containers with ErrNotManaged / not-found — never touches them.
func (a *Adapter) Start(ctx context.Context, container string) error {
	if _, err := a.verifyManaged(ctx, container); err != nil {
		return err
	}
	return a.runDocker(ctx, "start", container)
}

// Stop stops a managed container (STOP). Same ownership enforcement.
func (a *Adapter) Stop(ctx context.Context, container string) error {
	if _, err := a.verifyManaged(ctx, container); err != nil {
		return err
	}
	return a.runDocker(ctx, "stop", container)
}

// Remove removes a managed container (REMOVE). Idempotent: removing an
// already-absent container succeeds. Unmanaged containers are refused.
func (a *Adapter) Remove(ctx context.Context, container string) error {
	if _, err := a.verifyManaged(ctx, container); err != nil {
		if isNotFound(err) {
			return nil // already gone: idempotent
		}
		return err
	}
	return a.runDocker(ctx, "rm", container)
}

// Inspect returns the adapter's view of a container: running status, mapped
// ports, image digest and labels. Read-only: it does not require Axiom
// ownership (the dispatcher authorizes reads), but it reports the labels so
// callers can enforce ownership on mutations.
func (a *Adapter) Inspect(ctx context.Context, container string) (ContainerInfo, error) {
	if !ownership.ValidateName(container) {
		return ContainerInfo{}, &Error{Code: CodeInvalidInput, Message: "invalid container name"}
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	out, exit, err := a.runner().Run(ctx, "inspect", container)
	if ctx.Err() != nil {
		return ContainerInfo{}, &Error{Code: CodeInterrupted, Message: "inspect interrupted", Cause: ctx.Err()}
	}
	if exit != 0 {
		return ContainerInfo{}, &Error{Code: CodeContainerNotFound, Message: fmt.Sprintf("container %q not found", container), ExitCode: exit, Log: out}
	}
	if err != nil {
		return ContainerInfo{}, &Error{Code: CodeDockerFailed, Message: "docker inspect failed to start", Cause: err, Log: out}
	}
	var raw []containerInspect
	if err := json.Unmarshal([]byte(out), &raw); err != nil || len(raw) == 0 {
		return ContainerInfo{}, &Error{Code: CodeDockerFailed, Message: "docker inspect returned unparsable output", Log: tail(out, 4096)}
	}
	c := raw[0]
	info := ContainerInfo{
		Name:    strings.TrimPrefix(c.Name, "/"),
		Image:   c.Config.Image,
		ImageID: c.Image,
		Running: c.State.Running,
		Status:  c.State.Status,
		Labels:  c.Config.Labels,
		Ports:   parsePorts(c.HostConfig.PortBindings, c.NetworkSettings.Ports),
	}
	if info.Labels == nil {
		info.Labels = map[string]string{}
	}
	return info, nil
}

// Tail returns the last n lines of container logs (`docker logs --tail`).
// Read-only; the container name is validated and docker errors are
// normalized. The health/logs lanes (#85/#86) build on this.
func (a *Adapter) Tail(ctx context.Context, container string, n int) (string, error) {
	if !ownership.ValidateName(container) {
		return "", &Error{Code: CodeInvalidInput, Message: "invalid container name"}
	}
	if n <= 0 {
		n = 100 // bounded default
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	out, exit, err := a.runner().Run(ctx, "logs", "--tail", strconv.Itoa(n), container)
	if ctx.Err() != nil {
		return "", &Error{Code: CodeInterrupted, Message: "logs interrupted", Cause: ctx.Err()}
	}
	if exit != 0 {
		return "", &Error{Code: CodeDockerFailed, Message: fmt.Sprintf("docker logs exited with code %d", exit), ExitCode: exit, Log: out}
	}
	if err != nil {
		return "", &Error{Code: CodeDockerFailed, Message: "docker logs failed to start", Cause: err, Log: out}
	}
	return out, nil
}

// Cleanup removes every managed container in an operation scope. The listing
// query filters on the managed, application and deployment labels; each
// candidate is re-verified through inspect and
// ownership.CleanupScope.MayRemove before removal (defense in depth — a
// container that is not removable within the scope is skipped, never
// force-touched).
func (a *Adapter) Cleanup(ctx context.Context, applicationID, deploymentID string) error {
	if !applicationRe.MatchString(applicationID) {
		return &Error{Code: CodeInvalidInput, Message: "invalid applicationId"}
	}
	if !deploymentRe.MatchString(deploymentID) {
		return &Error{Code: CodeInvalidInput, Message: "invalid deploymentId"}
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	out, exit, err := a.runner().Run(ctx, "ps", "-a",
		"--filter", "label="+ownership.LabelManaged+"="+ownership.ManagedTrue,
		"--filter", "label="+ownership.LabelApplication+"="+applicationID,
		"--filter", "label="+ownership.LabelDeployment+"="+deploymentID,
		"--format", "{{.Names}}")
	if ctx.Err() != nil {
		return &Error{Code: CodeInterrupted, Message: "cleanup listing interrupted", Cause: ctx.Err()}
	}
	if exit != 0 {
		return &Error{Code: CodeDockerFailed, Message: fmt.Sprintf("docker ps exited with code %d", exit), ExitCode: exit, Log: out}
	}
	if err != nil {
		return &Error{Code: CodeDockerFailed, Message: "docker ps failed to start", Cause: err, Log: out}
	}
	scope := ownership.CleanupScope{ApplicationID: applicationID, DeploymentID: deploymentID}
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info, err := a.Inspect(ctx, name)
		if err != nil {
			if isNotFound(err) {
				continue // gone between listing and verification
			}
			return err // docker failure: refuse, touch nothing
		}
		if !scope.MayRemove(name, info.Labels) {
			continue // not removable within this scope: skip, never touch
		}
		if err := a.runDocker(ctx, "rm", "-f", name); err != nil {
			return err
		}
	}
	return nil
}

// ListManaged returns every Axiom-managed container on the host, whatever its
// deployment. It is the read surface startup reconciliation (#82) needs; the
// listing filters on the canonical managed label and each container is reported
// with its ownership labels so callers can decide per-deployment.
func (a *Adapter) ListManaged(ctx context.Context) ([]ContainerInfo, error) {
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	out, exit, err := a.runner().Run(ctx, "ps", "-a",
		"--filter", "label="+ownership.LabelManaged+"="+ownership.ManagedTrue,
		"--format", "{{.Names}}")
	if ctx.Err() != nil {
		return nil, &Error{Code: CodeInterrupted, Message: "managed listing interrupted", Cause: ctx.Err()}
	}
	if exit != 0 {
		return nil, &Error{Code: CodeDockerFailed, Message: fmt.Sprintf("docker ps exited with code %d", exit), ExitCode: exit, Log: out}
	}
	if err != nil {
		return nil, &Error{Code: CodeDockerFailed, Message: "docker ps failed to start", Cause: err, Log: out}
	}
	names := strings.Split(out, "\n")
	infos := make([]ContainerInfo, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info, err := a.Inspect(ctx, name)
		if err != nil {
			if isNotFound(err) {
				continue // gone between the listing and the inspection
			}
			return nil, err // docker failure: report, never guess
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// verifyManaged enforces the ownership boundary for every mutation: the
// container must exist AND be Axiom-managed (ownership.IsManaged). Anything
// else is refused (ErrNotManaged or not-found) and never touched.
func (a *Adapter) verifyManaged(ctx context.Context, container string) (ContainerInfo, error) {
	info, err := a.Inspect(ctx, container)
	if err != nil {
		return ContainerInfo{}, err
	}
	if !ownership.IsManaged(info.Labels) {
		return ContainerInfo{}, &Error{Code: CodeNotManaged, Message: fmt.Sprintf("container %q is not managed by axiom", container), Cause: ErrNotManaged}
	}
	return info, nil
}

// runDocker executes one docker subcommand and normalizes failures.
func (a *Adapter) runDocker(ctx context.Context, args ...string) error {
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	out, exit, err := a.runner().Run(ctx, args...)
	if ctx.Err() != nil {
		return &Error{Code: CodeInterrupted, Message: fmt.Sprintf("docker %s interrupted", args[0]), Cause: ctx.Err()}
	}
	if exit != 0 {
		return &Error{Code: CodeDockerFailed, Message: fmt.Sprintf("docker %s exited with code %d", args[0], exit), ExitCode: exit, Log: out}
	}
	if err != nil {
		return &Error{Code: CodeDockerFailed, Message: fmt.Sprintf("docker %s failed to start", args[0]), Cause: err, Log: out}
	}
	return nil
}

func isNotFound(err error) bool {
	var de *Error
	return errors.As(err, &de) && de.Code == CodeContainerNotFound
}

// labelPairs returns deterministic --label KEY=VALUE pairs: the canonical
// managed label set (ownership.NewLabels) first, then caller labels sorted.
// The managed labels can never be overridden through the caller's map.
func labelPairs(spec CreateSpec) []string {
	labels := ownership.NewLabels(spec.DeploymentID, spec.ApplicationID, spec.ServerID)
	for k, v := range spec.Labels {
		if k == ownership.LabelManaged || k == ownership.LabelDeployment ||
			k == ownership.LabelApplication || k == ownership.LabelServer || k == ownership.LabelCreated {
			continue // canonical labels are not overridable
		}
		labels[k] = v
	}
	pairs := make([]string, 0, len(labels))
	for _, k := range sortedKeys(labels) {
		pairs = append(pairs, k+"="+labels[k])
	}
	return pairs
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatCPUs(nano int) string {
	return strconv.FormatFloat(float64(nano)/1e9, 'f', -1, 64)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// containerInspect is the subset of `docker inspect` JSON the adapter needs.
type containerInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"Image"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		PortBindings map[string][]portBinding `json:"PortBindings"`
	} `json:"HostConfig"`
	State struct {
		Status  string `json:"Status"`
		Running bool   `json:"Running"`
	} `json:"State"`
	NetworkSettings struct {
		Ports map[string][]portBinding `json:"Ports"`
	} `json:"NetworkSettings"`
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// parsePorts flattens docker's port maps ("3000/tcp" -> bindings) into
// deterministic mappings sorted by container port. Allocated bindings
// (NetworkSettings.Ports, populated once the container starts) win over the
// configured ones (HostConfig.PortBindings, present at create time with an
// empty HostPort for ephemeral bindings).
func parsePorts(configured, allocated map[string][]portBinding) []PortMapping {
	var out []PortMapping
	allocatedCP := map[int]bool{}
	for key, bindings := range allocated {
		if len(bindings) == 0 {
			continue
		}
		cp, err := strconv.Atoi(strings.TrimSuffix(key, "/tcp"))
		if err != nil {
			continue
		}
		b := bindings[0]
		hp, _ := strconv.Atoi(b.HostPort)
		out = append(out, PortMapping{HostIP: b.HostIP, HostPort: hp, ContainerPort: cp})
		allocatedCP[cp] = true
	}
	for key, bindings := range configured {
		if len(bindings) == 0 {
			continue
		}
		cp, err := strconv.Atoi(strings.TrimSuffix(key, "/tcp"))
		if err != nil {
			continue
		}
		if allocatedCP[cp] {
			continue // the allocated binding supersedes the configured one
		}
		out = append(out, PortMapping{HostIP: bindings[0].HostIP, HostPort: 0, ContainerPort: cp})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ContainerPort < out[j].ContainerPort })
	return out
}
