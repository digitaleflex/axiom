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
	// Env is the process environment of the docker child. When nil the
	// parent environment is used; set it explicitly to isolate builds.
	Env       []string
	MaxLogLen int // tail of build output kept in the result
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
	if b.Env != nil {
		cmd.Env = b.Env
	}
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
