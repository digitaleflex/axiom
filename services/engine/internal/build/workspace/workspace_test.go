package workspace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type entry struct {
	name string
	body string
	typ  byte
	link string
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o777, Size: int64(len(e.body)), Linkname: e.link}
		if typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func manager(t *testing.T) *Manager {
	t.Helper()
	return &Manager{Root: t.TempDir(), Limits: DefaultLimits}
}

func TestCreateIsUniqueAndPrivate(t *testing.T) {
	m := manager(t)
	a, err := m.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Cleanup()
	b, err := m.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	if a.Dir == b.Dir {
		t.Fatal("workspace directories must be unique")
	}
	for _, w := range []*Workspace{a, b} {
		fi, err := os.Stat(w.Dir)
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Fatalf("workspace %s must be 0700: %v %v", w.Dir, fi, err)
		}
	}
}

func TestExtractStripsTopLevelAndIgnoresExecBits(t *testing.T) {
	m := manager(t)
	w, _ := m.Create(context.Background())
	defer w.Cleanup()
	data := archive(t,
		entry{name: "repo-abc/", typ: tar.TypeDir},
		entry{name: "repo-abc/app/main.go", body: "package main"},
		entry{name: "repo-abc/run.sh", body: "#!/bin/sh\necho hi"},
		entry{name: "repo-abc/link", typ: tar.TypeSymlink, link: "/etc/passwd"},
	)
	skipped, err := w.ExtractTarGz(context.Background(), bytes.NewReader(data), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0] != "link" {
		t.Fatalf("symlink must be skipped, got %v", skipped)
	}
	for _, p := range []string{"app/main.go", "run.sh"} {
		if _, err := os.Stat(filepath.Join(w.Dir, p)); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}
	fi, _ := os.Stat(filepath.Join(w.Dir, "run.sh"))
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("archive exec bits must not be honored: %o", fi.Mode().Perm())
	}
	if _, err := os.Lstat(filepath.Join(w.Dir, "link")); !os.IsNotExist(err) {
		t.Fatal("symlink must not be created")
	}
}

func TestTraversalRejectedAndCleanedUp(t *testing.T) {
	m := manager(t)
	w, _ := m.Create(context.Background())
	dir := w.Dir
	data := archive(t, entry{name: "repo/../../evil", body: "x"})
	if _, err := w.ExtractTarGz(context.Background(), bytes.NewReader(data), DefaultLimits); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("want ErrUnsafePath, got %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("failed extraction must clean up the workspace")
	}
	// Cleanup is idempotent.
	if err := w.Cleanup(); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
}

func TestLimitsAndEmpty(t *testing.T) {
	m := manager(t)
	big := strings.Repeat("a", 64<<10)
	data := archive(t, entry{name: "repo/blob.bin", body: big})
	w, _ := m.Create(context.Background())
	lim := DefaultLimits
	lim.MaxFileBytes = 1024
	if _, err := w.ExtractTarGz(context.Background(), bytes.NewReader(data), lim); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized file: %v", err)
	}
	w, _ = m.Create(context.Background())
	lim = DefaultLimits
	lim.MaxFiles = 1
	if _, err := w.ExtractTarGz(context.Background(), bytes.NewReader(archive(t, entry{name: "repo/a", body: "1"}, entry{name: "repo/b", body: "2"})), lim); !errors.Is(err, ErrTooMany) {
		t.Fatalf("too many files: %v", err)
	}
	w, _ = m.Create(context.Background())
	if _, err := w.ExtractTarGz(context.Background(), bytes.NewReader(archive(t, entry{name: "repo/", typ: tar.TypeDir})), DefaultLimits); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := w.ExtractTarGz(context.Background(), strings.NewReader("nope"), DefaultLimits); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed: %v", err)
	}
}

func TestCancellation(t *testing.T) {
	m := manager(t)
	w, _ := m.Create(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.ExtractTarGz(ctx, bytes.NewReader(archive(t, entry{name: "repo/a", body: "1"})), DefaultLimits); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Fatal("cancelled extraction must clean up")
	}
	if _, err := m.Create(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("create with cancelled context: %v", err)
	}
}

func TestConcurrentBuilds(t *testing.T) {
	m := manager(t)
	var wg sync.WaitGroup
	dirs := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, err := m.Create(context.Background())
			if err != nil {
				t.Errorf("create: %v", err)
				return
			}
			defer w.Cleanup()
			if _, err := w.ExtractTarGz(context.Background(), bytes.NewReader(archive(t, entry{name: "repo/a", body: "1"})), DefaultLimits); err != nil {
				t.Errorf("extract: %v", err)
				return
			}
			dirs <- w.Dir
		}()
	}
	wg.Wait()
	close(dirs)
	seen := map[string]bool{}
	for d := range dirs {
		if seen[d] {
			t.Fatalf("duplicate workspace %s", d)
		}
		seen[d] = true
	}
	if len(seen) != 16 {
		t.Fatalf("got %d workspaces", len(seen))
	}
	entries, _ := os.ReadDir(m.Root)
	if len(entries) != 0 {
		t.Fatalf("all workspaces must be cleaned up, left %d", len(entries))
	}
}

func TestCreateRequiresRoot(t *testing.T) {
	if _, err := (&Manager{}).Create(context.Background()); err == nil {
		t.Fatal("empty root must be rejected")
	}
}
