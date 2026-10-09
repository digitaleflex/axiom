package build

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// BuildSpec describes one image build. No secrets are carried: build-time
// values must already be inside the context or come from secret references
// resolved by the caller (#126).
type BuildSpec struct {
	ContextDir string
	Dockerfile string // relative to ContextDir
	Tags       []string
	Labels     map[string]string
	Timeout    time.Duration
}

// ImageResult is the immutable output of a successful build.
type ImageResult struct {
	ImageRef   string
	ImageID    string // sha256:… from --iidfile
	DurationMs int64
	Log        string // tail of the build output (bounded)
}

// ImageBuilder builds an image from a context directory.
type ImageBuilder interface {
	Build(ctx context.Context, spec BuildSpec) (ImageResult, error)
}

// ExecBuilder invokes the Docker CLI directly (no shell). The command line is
// built from fixed flags only; context and tag values are passed as argv.
type ExecBuilder struct {
	// Docker is the executable path. Empty means "docker" from PATH.
	Docker string
	// Env holds extra KEY=VALUE entries for the docker child. It is applied
	// on top of the minimal build environment built by childEnv and overrides
	// it on key collision. It never re-enables inheritance: the Engine's own
	// process environment is NOT passed to the child in any case, so secrets
	// such as AXIOM_SECRET_KEY, DATABASE_URL or AXIOM_API_TOKEN can never
	// reach `docker build` (see childEnv for the exact rules).
	Env       []string
	MaxLogLen int // tail of build output kept in the result
}

// defaultPATH is used for the child when the parent has no PATH of its own.
const defaultPATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// buildEnvNames is the complete allowlist of host variables the docker child
// may receive. Everything else in the Engine process environment is dropped:
// AXIOM_SECRET_KEY, DATABASE_URL, AXIOM_API_TOKEN, GitHub credentials and any
// other operator-set variable must never be visible to a Dockerfile executed
// during `docker build`. Build-time values belong in the build context or in
// secret references resolved by the caller (#126), never in this list.
var buildEnvNames = []string{
	// Minimal process basics.
	"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL",
	// Docker CLI connectivity (daemon endpoint and client TLS material).
	"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY",
	// Egress configuration needed to pull base images. Operators must not
	// embed credentials in proxy URLs; those would be visible to the build.
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	"http_proxy", "https_proxy", "no_proxy",
	// Go module proxy settings, used when a build invokes the Go toolchain.
	"GOPROXY", "GOPRIVATE", "GOSUMDB",
}

var buildEnvAllowed = func() map[string]bool {
	m := make(map[string]bool, len(buildEnvNames))
	for _, n := range buildEnvNames {
		m[n] = true
	}
	return m
}()

// childEnv computes the explicit environment of the docker child. The parent
// (Engine) environment is never inherited wholesale: only names present in
// buildEnvAllowed are copied from parent. Entries in extra (ExecBuilder.Env)
// are then applied on top and override the base on key collision, so extra is
// the complete caller-requested set on top of the minimal base. A PATH default
// is added when neither parent nor extra provides one.
func childEnv(parent []string, extra []string) []string {
	values := make(map[string]string, len(buildEnvNames)+len(extra))
	order := make([]string, 0, len(buildEnvNames)+len(extra))
	put := func(kv string) {
		name, val, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			return
		}
		if _, seen := values[name]; !seen {
			order = append(order, name)
		}
		values[name] = val
	}
	for _, kv := range parent {
		if name, _, ok := strings.Cut(kv, "="); ok && buildEnvAllowed[name] {
			put(kv)
		}
	}
	for _, kv := range extra {
		put(kv)
	}
	if _, ok := values["PATH"]; !ok {
		put("PATH=" + defaultPATH)
	}
	env := make([]string, 0, len(order))
	for _, name := range order {
		env = append(env, name+"="+values[name])
	}
	return env
}

func (b *ExecBuilder) dockerBin() string {
	if b.Docker != "" {
		return b.Docker
	}
	return "docker"
}

func (b *ExecBuilder) Build(ctx context.Context, spec BuildSpec) (ImageResult, error) {
	if spec.ContextDir == "" || spec.Dockerfile == "" || len(spec.Tags) == 0 {
		return ImageResult{}, &Error{Code: CodeInvalidInput, Message: "context directory, Dockerfile and at least one tag are required"}
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	iid, err := os.CreateTemp("", "axiom-iid-*")
	if err != nil {
		return ImageResult{}, &Error{Code: CodeInternal, Message: "create iidfile", Cause: err}
	}
	iidPath := iid.Name()
	_ = iid.Close()
	defer os.Remove(iidPath)

	args := []string{"build", "--iidfile", iidPath, "-f", spec.Dockerfile}
	for _, t := range spec.Tags {
		args = append(args, "-t", t)
	}
	for k, v := range spec.Labels {
		args = append(args, "--label", k+"="+v)
	}
	args = append(args, spec.ContextDir)

	cmd := exec.CommandContext(ctx, b.dockerBin(), args...)
	cmd.Dir = spec.ContextDir
	// Always set an explicit environment: a nil cmd.Env would make the child
	// inherit the whole Engine environment (secrets included).
	cmd.Env = childEnv(os.Environ(), b.Env)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)
	log := tail(out.String(), b.maxLogLen())

	if runErr != nil {
		exit := -1
		if ee, ok := runErr.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		}
		if ctx.Err() != nil {
			return ImageResult{}, &Error{Code: CodeInterrupted, Message: "build interrupted", Cause: ctx.Err(), Log: log}
		}
		return ImageResult{}, &Error{Code: CodeBuildFailed, Message: fmt.Sprintf("docker build exited with code %d", exit), ExitCode: exit, Log: log}
	}
	raw, err := os.ReadFile(iidPath)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(raw)), "sha256:") {
		return ImageResult{}, &Error{Code: CodeInternal, Message: "docker build did not report an image ID", Log: log}
	}
	return ImageResult{ImageRef: spec.Tags[0], ImageID: strings.TrimSpace(string(raw)), DurationMs: duration.Milliseconds(), Log: log}, nil
}

func (b *ExecBuilder) maxLogLen() int {
	if b.MaxLogLen > 0 {
		return b.MaxLogLen
	}
	return 32 << 10
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
