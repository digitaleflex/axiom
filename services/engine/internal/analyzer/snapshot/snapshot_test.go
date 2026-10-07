package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
)

type entry struct {
	name    string
	body    string
	typ     byte
	link    string
	rawSize int64
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "3f9c2a1"}})
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Size: int64(len(e.body)), Linkname: e.link}
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

func TestNodeFixture(t *testing.T) {
	data := archive(t,
		entry{name: "acme-web-3f9c2a1/", typ: tar.TypeDir},
		entry{name: "acme-web-3f9c2a1/package.json", body: `{"dependencies":{"next":"14.2.0"},"scripts":{"build":"next build"}}`},
		entry{name: "acme-web-3f9c2a1/pnpm-lock.yaml", body: "lockfileVersion: '9.0'\n"},
		entry{name: "acme-web-3f9c2a1/public/logo.png", body: "\x89PNG\r\n\x1a\n\x00\x00binary"},
		entry{name: "acme-web-3f9c2a1/app/page.tsx", body: "export default function Page() { return null }"},
		entry{name: "acme-web-3f9c2a1/node_modules_link", typ: tar.TypeSymlink, link: "/etc/passwd"},
	)
	s, err := FromTarGz(bytes.NewReader(data), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range s.Files {
		paths = append(paths, f.Path)
	}
	if got := strings.Join(paths, ","); got != "app/page.tsx,package.json,pnpm-lock.yaml,public/logo.png" {
		t.Fatalf("paths = %s", got)
	}
	pkg, ok := s.Lookup("package.json")
	if !ok || !pkg.Retained || !strings.Contains(string(pkg.Content), "next") {
		t.Fatalf("package.json = %+v", pkg)
	}
	logo, _ := s.Lookup("public/logo.png")
	if !logo.Binary || logo.Retained || logo.Content != nil {
		t.Fatalf("binary file must not be retained: %+v", logo)
	}
	if len(s.Skipped) != 1 || s.Skipped[0] != "node_modules_link" {
		t.Fatalf("symlinks must be skipped, got %v", s.Skipped)
	}
}

func TestGoFixtureAndCorrelation(t *testing.T) {
	data := archive(t,
		entry{name: "svc-abc/go.mod", body: "module example.com/svc\n\ngo 1.23\n"},
		entry{name: "svc-abc/cmd/server/main.go", body: "package main\nfunc main() {}\n"},
	)
	s, err := Fetch(context.Background(), fakeSource(data), "usr_1", "repo_1", "main", strings.Repeat("a", 40), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if s.RepositoryID != "repo_1" || s.Ref != "main" || s.Commit != strings.Repeat("a", 40) || len(s.Files) != 2 {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestTraversalAndUnsafePathsRejected(t *testing.T) {
	for _, name := range []string{"repo/../../etc/passwd", "/abs/file", "repo/a\\b", "repo/x/../../y"} {
		data := archive(t, entry{name: name, body: "x"})
		if _, err := FromTarGz(bytes.NewReader(data), DefaultLimits); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("%q: want ErrUnsafePath, got %v", name, err)
		}
	}
	data := archive(t, entry{name: "repo/a.txt", body: "1"}, entry{name: "repo/./a.txt", body: "2"})
	if _, err := FromTarGz(bytes.NewReader(data), DefaultLimits); !errors.Is(err, ErrMalformed) {
		t.Errorf("duplicate entries must be rejected, got %v", err)
	}
}

func TestOversizedSourcesFailSafely(t *testing.T) {
	big := make([]byte, 64<<10)
	_, _ = rand.Read(big) // incompressible
	data := archive(t, entry{name: "repo/blob.bin", body: string(big)})

	if _, err := FromTarGz(bytes.NewReader(data), Limits{MaxArchiveBytes: 1 << 10, MaxTotalBytes: 1 << 30, MaxFiles: 10, MaxRetainedBytes: 1 << 10, MaxRetainedTotal: 1 << 20}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("compressed limit: %v", err)
	}
	if _, err := FromTarGz(bytes.NewReader(data), Limits{MaxArchiveBytes: 1 << 30, MaxTotalBytes: 1 << 10, MaxFiles: 10, MaxRetainedBytes: 1 << 10, MaxRetainedTotal: 1 << 20}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("uncompressed limit: %v", err)
	}
	many := archive(t, entry{name: "repo/a", body: "1"}, entry{name: "repo/b", body: "2"}, entry{name: "repo/c", body: "3"})
	if _, err := FromTarGz(bytes.NewReader(many), Limits{MaxArchiveBytes: 1 << 30, MaxTotalBytes: 1 << 30, MaxFiles: 2, MaxRetainedBytes: 1 << 10, MaxRetainedTotal: 1 << 20}); !errors.Is(err, ErrTooMany) {
		t.Errorf("file count limit: %v", err)
	}
	// Large text files are kept as metadata only.
	text := strings.Repeat("a", 4<<10)
	s, err := FromTarGz(bytes.NewReader(archive(t, entry{name: "repo/big.txt", body: text})), Limits{MaxArchiveBytes: 1 << 30, MaxTotalBytes: 1 << 30, MaxFiles: 10, MaxRetainedBytes: 1 << 10, MaxRetainedTotal: 1 << 20})
	if err != nil || s.Files[0].Retained || s.Files[0].Size != int64(len(text)) {
		t.Fatalf("large text file: %+v %v", s.Files, err)
	}
}

func TestMalformedAndEmpty(t *testing.T) {
	if _, err := FromTarGz(strings.NewReader("not gzip"), DefaultLimits); !errors.Is(err, ErrMalformed) {
		t.Errorf("not gzip: %v", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte("not a tar archive at all, just text that is long enough to be read as a header block?"))
	_ = gz.Close()
	if _, err := FromTarGz(&buf, DefaultLimits); !errors.Is(err, ErrMalformed) {
		t.Errorf("not tar: %v", err)
	}
	if _, err := FromTarGz(bytes.NewReader(archive(t, entry{name: "repo/", typ: tar.TypeDir})), DefaultLimits); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty repository: %v", err)
	}
	truncated := archive(t, entry{name: "repo/a.txt", body: strings.Repeat("x", 10000)})
	if _, err := FromTarGz(bytes.NewReader(truncated[:len(truncated)/2]), DefaultLimits); err == nil {
		t.Error("truncated archive must fail")
	}
}

type fakeSource []byte

func (f fakeSource) Archive(context.Context, string, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f)), nil
}

func TestNormalizeRejectsControlAndInvalidNames(t *testing.T) {
	for _, name := range []string{"repo/ok\x00", "repo/\xff\xfe", ""} {
		if _, _, err := normalize(name); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("%q: want ErrUnsafePath, got %v", name, err)
		}
	}
	if p, ok, err := normalize("repo/src//./main.go"); err != nil || !ok || p != "src/main.go" {
		t.Errorf("clean path = %q %v %v", p, ok, err)
	}
}
