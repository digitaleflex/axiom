// Package capabilities discovers and reports the host capabilities and
// resources the Agent needs for deployment target selection (#79): Docker,
// Docker Compose, Traefik, TLS, CPU, memory, free disk, architecture and
// OS. Discovery never fails: absent tools are reported as unavailable and
// unreadable probes as zero values. Every report carries a collection
// timestamp so the Engine can distinguish stale reports from current ones.
package capabilities

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Capability vocabulary shared with the Engine (docs/architecture/
// agent-protocol.md §8). Reports list the subset actually available.
const (
	CapDocker        = "docker"
	CapDockerCompose = "docker_compose"
	CapTraefik       = "traefik"
	CapTLS           = "tls"
)

// DefaultTimeout bounds each discovery command.
const DefaultTimeout = 5 * time.Second

// Runner executes one fixed command with fixed argv and returns its
// trimmed stdout. Discovery never uses a shell; absence of the binary or a
// non-zero exit is tolerated (reported as unavailable).
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner is the production Runner: it executes the fixed argv directly
// (no shell) under a timeout.
type ExecRunner struct {
	Timeout time.Duration
}

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// FuncRunner adapts a function to Runner, so tests can fake every command.
type FuncRunner func(ctx context.Context, name string, args ...string) (string, error)

// Run implements Runner.
func (f FuncRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

// Report is the normalized capability document the Agent sends at
// registration and heartbeat.
type Report struct {
	AgentVersion string      `json:"agentVersion"`
	CollectedAt  time.Time   `json:"collectedAt"`
	Docker       DockerInfo  `json:"docker"`
	Traefik      TraefikInfo `json:"traefik"`
	TLS          TLSInfo     `json:"tls"`
	CPUCount     int         `json:"cpuCount"`
	MemoryMB     int         `json:"memoryMb"`
	DiskFreeMB   int         `json:"diskFreeMb"`
	Arch         string      `json:"arch"`
	OS           string      `json:"os"`
}

// DockerInfo describes the Docker toolchain on this host.
type DockerInfo struct {
	Available        bool   `json:"available"`
	Version          string `json:"version,omitempty"`
	ComposeAvailable bool   `json:"composeAvailable"`
	ComposeVersion   string `json:"composeVersion,omitempty"`
}

// TraefikInfo describes the Traefik binary on this host.
type TraefikInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
}

// TLSInfo describes TLS capability. Automatic is true when Traefik is
// available to terminate TLS.
type TLSInfo struct {
	Automatic bool `json:"automatic"`
}

// Capabilities returns the engine-compatible capability vocabulary subset
// that is actually available: docker, docker_compose, traefik, and tls
// (tls only when Traefik is present).
func (r Report) Capabilities() []string {
	var caps []string
	if r.Docker.Available {
		caps = append(caps, CapDocker)
	}
	if r.Docker.ComposeAvailable {
		caps = append(caps, CapDockerCompose)
	}
	if r.Traefik.Available {
		caps = append(caps, CapTraefik)
	}
	if r.TLS.Automatic {
		caps = append(caps, CapTLS)
	}
	return caps
}

// IsStale reports whether the report was collected more than maxAge before
// now.
func (r Report) IsStale(now time.Time, maxAge time.Duration) bool {
	return now.Sub(r.CollectedAt) > maxAge
}

// Discoverer collects a Report. The Runner and DataRoot are injectable so
// tests can fake every command and point the disk probe at a temp dir.
type Discoverer struct {
	AgentVersion string
	Runner       Runner
	DataRoot     string
}

// NewDiscoverer returns a Discoverer with the production ExecRunner.
func NewDiscoverer(agentVersion, dataRoot string) Discoverer {
	return Discoverer{
		AgentVersion: agentVersion,
		Runner:       ExecRunner{Timeout: DefaultTimeout},
		DataRoot:     dataRoot,
	}
}

// Discover collects the capability report. It never fails: absent tools are
// reported as unavailable and unreadable probes as zero values.
func (d Discoverer) Discover(ctx context.Context) Report {
	if d.Runner == nil {
		d.Runner = ExecRunner{Timeout: DefaultTimeout}
	}
	dataRoot := d.DataRoot
	if dataRoot == "" {
		dataRoot = "/"
	}

	docker := d.discoverDocker(ctx)
	traefik := d.discoverTraefik(ctx)

	return Report{
		AgentVersion: d.AgentVersion,
		CollectedAt:  time.Now().UTC(),
		Docker:       docker,
		Traefik:      traefik,
		TLS:          TLSInfo{Automatic: traefik.Available},
		CPUCount:     runtime.NumCPU(),
		MemoryMB:     memoryMB(),
		DiskFreeMB:   diskFreeMB(dataRoot),
		Arch:         runtime.GOARCH,
		OS:           runtime.GOOS,
	}
}

// discoverDocker probes `docker version` and `docker compose version` with
// fixed argv. Absence of either is tolerated.
func (d Discoverer) discoverDocker(ctx context.Context) DockerInfo {
	info := DockerInfo{}
	if version, err := d.Runner.Run(ctx, "docker", "version", "--format", "{{.Server.Version}}"); err == nil {
		info.Available = true
		info.Version = version
	}
	if composeVersion, err := d.Runner.Run(ctx, "docker", "compose", "version", "--short"); err == nil {
		info.ComposeAvailable = true
		info.ComposeVersion = composeVersion
	}
	return info
}

// discoverTraefik probes `traefik version`. Absence is tolerated.
func (d Discoverer) discoverTraefik(ctx context.Context) TraefikInfo {
	out, err := d.Runner.Run(ctx, "traefik", "version")
	if err != nil {
		return TraefikInfo{}
	}
	return TraefikInfo{Available: true, Version: parseTraefikVersion(out)}
}

// parseTraefikVersion extracts the version from `traefik version` output (a
// "Version: vX.Y.Z" line), falling back to the trimmed output.
func parseTraefikVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(out)
}

// memoryMB reads /proc/meminfo and returns total memory in MB. Unreadable
// or missing data yields 0.
func memoryMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	return parseMeminfoMB(f)
}

// parseMeminfoMB extracts MemTotal (kB) from meminfo content, converting to
// MB. Missing or malformed data yields 0.
func parseMeminfoMB(r io.Reader) int {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return int(kb / 1024)
	}
	return 0
}

// diskFreeMB returns the free space of the filesystem containing path, in
// MB. An unreadable path yields 0.
func diskFreeMB(path string) int {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	return int(uint64(stat.Bavail) * uint64(stat.Bsize) / (1024 * 1024))
}
