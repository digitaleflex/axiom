package capabilities

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stubRunner returns a FuncRunner that answers only the given "name args"
// keys and reports every other command as unavailable.
func stubRunner(responses map[string]string) FuncRunner {
	return func(_ context.Context, name string, args ...string) (string, error) {
		key := name + " " + strings.Join(args, " ")
		if out, ok := responses[key]; ok {
			return out, nil
		}
		return "", fmt.Errorf("command not available: %s", key)
	}
}

func TestDiscoverDockerAndComposePresent(t *testing.T) {
	d := Discoverer{
		AgentVersion: "0.1.0",
		Runner: stubRunner(map[string]string{
			"docker version --format {{.Server.Version}}": "27.0.1",
			"docker compose version --short":              "v2.27.0",
		}),
		DataRoot: t.TempDir(),
	}
	report := d.Discover(context.Background())

	if !report.Docker.Available || report.Docker.Version != "27.0.1" {
		t.Errorf("Docker = %+v, want available 27.0.1", report.Docker)
	}
	if !report.Docker.ComposeAvailable || report.Docker.ComposeVersion != "v2.27.0" {
		t.Errorf("Compose = %+v, want available v2.27.0", report.Docker)
	}
	if report.Traefik.Available {
		t.Error("Traefik must be unavailable when the command is absent")
	}
	if report.TLS.Automatic {
		t.Error("TLS.Automatic must be false without Traefik")
	}
	want := []string{CapDocker, CapDockerCompose}
	got := report.Capabilities()
	if len(got) != len(want) {
		t.Fatalf("Capabilities = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Capabilities = %v, want %v", got, want)
		}
	}
}

func TestDiscoverDockerPresentComposeAbsent(t *testing.T) {
	d := Discoverer{
		AgentVersion: "0.1.0",
		Runner: stubRunner(map[string]string{
			"docker version --format {{.Server.Version}}": "24.0.7",
		}),
		DataRoot: t.TempDir(),
	}
	report := d.Discover(context.Background())

	if !report.Docker.Available || report.Docker.Version != "24.0.7" {
		t.Errorf("Docker = %+v, want available 24.0.7", report.Docker)
	}
	if report.Docker.ComposeAvailable {
		t.Error("Compose must be unavailable when the command is absent")
	}
	if got := report.Capabilities(); len(got) != 1 || got[0] != CapDocker {
		t.Errorf("Capabilities = %v, want [docker]", got)
	}
}

func TestDiscoverTraefikPresent(t *testing.T) {
	d := Discoverer{
		AgentVersion: "0.1.0",
		Runner: stubRunner(map[string]string{
			"docker version --format {{.Server.Version}}": "27.0.1",
			"traefik version": "Version:      v3.1.0\nCodename:     raclette\nGo version:   go1.22.1",
		}),
		DataRoot: t.TempDir(),
	}
	report := d.Discover(context.Background())

	if !report.Traefik.Available || report.Traefik.Version != "v3.1.0" {
		t.Errorf("Traefik = %+v, want available v3.1.0", report.Traefik)
	}
	if !report.TLS.Automatic {
		t.Error("TLS.Automatic must be true when Traefik is available")
	}
	got := report.Capabilities()
	want := []string{CapDocker, CapTraefik, CapTLS}
	if len(got) != len(want) {
		t.Fatalf("Capabilities = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Capabilities = %v, want %v", got, want)
		}
	}
}

// TestDiscoverAllAbsent simulates a host with no Docker toolchain and no
// Traefik: discovery must tolerate it, report everything unavailable, and
// never fail or hang.
func TestDiscoverAllAbsent(t *testing.T) {
	d := Discoverer{
		AgentVersion: "0.1.0",
		Runner:       stubRunner(map[string]string{}),
		DataRoot:     t.TempDir(),
	}
	report := d.Discover(context.Background())

	if report.Docker.Available || report.Docker.ComposeAvailable || report.Traefik.Available {
		t.Errorf("all tools must be unavailable, got %+v", report)
	}
	if report.TLS.Automatic {
		t.Error("TLS.Automatic must be false when nothing is available")
	}
	if caps := report.Capabilities(); len(caps) != 0 {
		t.Errorf("Capabilities = %v, want empty", caps)
	}
	if report.CollectedAt.IsZero() {
		t.Error("CollectedAt must be set")
	}
	if report.AgentVersion != "0.1.0" {
		t.Errorf("AgentVersion = %q, want 0.1.0", report.AgentVersion)
	}
	if report.CPUCount != runtime.NumCPU() {
		t.Errorf("CPUCount = %d, want %d", report.CPUCount, runtime.NumCPU())
	}
	if report.Arch != runtime.GOARCH || report.OS != runtime.GOOS {
		t.Errorf("Arch/OS = %s/%s, want %s/%s", report.Arch, report.OS, runtime.GOARCH, runtime.GOOS)
	}
	if report.MemoryMB <= 0 {
		t.Errorf("MemoryMB = %d, want > 0 from /proc/meminfo", report.MemoryMB)
	}
	if report.DiskFreeMB <= 0 {
		t.Errorf("DiskFreeMB = %d, want > 0 for temp dir", report.DiskFreeMB)
	}
}

func TestParseTraefikVersion(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Version:      v3.1.0\nCodename: raclette", "v3.1.0"},
		{"Version: v2.10.4", "v2.10.4"},
		{"v3.0.0", "v3.0.0"}, // no Version: line: fall back to trimmed output
		{"", ""},
	}
	for _, tt := range tests {
		if got := parseTraefikVersion(tt.in); got != tt.want {
			t.Errorf("parseTraefikVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseMeminfoMB(t *testing.T) {
	fixture := `MemFree:          1024 kB
MemTotal:       16384256 kB
SwapTotal:             0 kB
`
	if got := parseMeminfoMB(strings.NewReader(fixture)); got != 16384256/1024 {
		t.Errorf("parseMeminfoMB = %d, want %d", got, 16384256/1024)
	}

	if got := parseMeminfoMB(strings.NewReader("MemFree: 1 kB\n")); got != 0 {
		t.Errorf("parseMeminfoMB(without MemTotal) = %d, want 0", got)
	}
	if got := parseMeminfoMB(strings.NewReader("MemTotal: notanumber kB\n")); got != 0 {
		t.Errorf("parseMeminfoMB(malformed) = %d, want 0", got)
	}
	if got := parseMeminfoMB(strings.NewReader("")); got != 0 {
		t.Errorf("parseMeminfoMB(empty) = %d, want 0", got)
	}
}

func TestDiskFreeMB(t *testing.T) {
	if got := diskFreeMB(t.TempDir()); got <= 0 {
		t.Errorf("diskFreeMB(temp dir) = %d, want > 0", got)
	}
	if got := diskFreeMB(filepath.Join(t.TempDir(), "does-not-exist")); got != 0 {
		t.Errorf("diskFreeMB(missing path) = %d, want 0", got)
	}
}

func TestIsStale(t *testing.T) {
	now := time.Now().UTC()
	fresh := Report{CollectedAt: now.Add(-30 * time.Second)}
	if fresh.IsStale(now, time.Minute) {
		t.Error("30s-old report must not be stale at 1m maxAge")
	}
	stale := Report{CollectedAt: now.Add(-2 * time.Hour)}
	if !stale.IsStale(now, time.Hour) {
		t.Error("2h-old report must be stale at 1h maxAge")
	}
	future := Report{CollectedAt: now.Add(time.Hour)}
	if future.IsStale(now, time.Hour) {
		t.Error("future-dated report must not be stale")
	}
}

func TestExecRunnerAbsentBinary(t *testing.T) {
	_, err := ExecRunner{Timeout: time.Second}.Run(context.Background(), "axiom-no-such-binary-xyz")
	if err == nil {
		t.Fatal("running a non-existent binary must return an error")
	}
}

func TestNewDiscoverer(t *testing.T) {
	d := NewDiscoverer("1.2.3", "/tmp")
	if d.AgentVersion != "1.2.3" || d.DataRoot != "/tmp" {
		t.Errorf("NewDiscoverer = %+v, want version 1.2.3 and root /tmp", d)
	}
	if _, ok := d.Runner.(ExecRunner); !ok {
		t.Errorf("NewDiscoverer Runner = %T, want ExecRunner", d.Runner)
	}
}

// TestDiscoverNilRunner ensures Discover falls back to the production
// runner instead of panicking when the Runner is not injected.
func TestDiscoverNilRunner(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Discover with nil Runner panicked: %v", r)
		}
	}()
	report := Discoverer{AgentVersion: "0.1.0", DataRoot: t.TempDir()}.Discover(context.Background())
	if report.CollectedAt.IsZero() {
		t.Error("CollectedAt must be set")
	}
}

// TestDiscoverDataRootInjected verifies the disk probe uses the injected
// data root (temp dir) rather than a hardcoded path.
func TestDiscoverDataRootInjected(t *testing.T) {
	dir := t.TempDir()
	report := Discoverer{
		AgentVersion: "0.1.0",
		Runner:       stubRunner(map[string]string{}),
		DataRoot:     dir,
	}.Discover(context.Background())
	if report.DiskFreeMB != diskFreeMB(dir) {
		t.Errorf("DiskFreeMB = %d, want %d from Statfs(%s)", report.DiskFreeMB, diskFreeMB(dir), dir)
	}
}
