// Package snapshot builds a bounded, read-only RepositorySnapshot from a
// repository archive (issue #93). It never executes project code, never
// follows links, rejects path traversal and fails safely on malformed or
// oversized input. The analyzer consumes snapshots only (GitHub-agnostic).
package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// Errors returned for unsafe or unusable sources.
var (
	ErrTooLarge   = errors.New("snapshot: repository exceeds size limits")
	ErrTooMany    = errors.New("snapshot: repository has too many files")
	ErrMalformed  = errors.New("snapshot: malformed archive")
	ErrUnsafePath = errors.New("snapshot: unsafe path in archive")
	ErrEmpty      = errors.New("snapshot: repository is empty")
)

// Limits bound resource usage while reading an archive.
type Limits struct {
	MaxArchiveBytes  int64 // compressed bytes read from the source
	MaxTotalBytes    int64 // sum of uncompressed file sizes
	MaxFiles         int
	MaxRetainedBytes int64 // per-file content kept in memory for analysis
	MaxRetainedTotal int64 // total content kept in memory
}

// DefaultLimits are suitable for V0.1 application repositories.
var DefaultLimits = Limits{
	MaxArchiveBytes:  200 << 20,
	MaxTotalBytes:    1 << 30,
	MaxFiles:         50_000,
	MaxRetainedBytes: 512 << 10,
	MaxRetainedTotal: 64 << 20,
}

// File is one regular file. Content is retained only for small text files
// that analysis may need; others keep metadata only.
type File struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Binary   bool   `json:"binary"`
	Content  []byte `json:"-"`
	Retained bool   `json:"retained"`
}

// Snapshot is the immutable analysis input, correlated to an exact commit.
type Snapshot struct {
	RepositoryID string   `json:"repositoryId"`
	Ref          string   `json:"ref"`
	Commit       string   `json:"commit"`
	Files        []File   `json:"files"`   // sorted by path
	Skipped      []string `json:"skipped"` // symlinks, devices… (never followed)
	TotalBytes   int64    `json:"totalBytes"`
}

// Lookup returns the file at path p.
func (s Snapshot) Lookup(p string) (File, bool) {
	i := sort.Search(len(s.Files), func(i int) bool { return s.Files[i].Path >= p })
	if i < len(s.Files) && s.Files[i].Path == p {
		return s.Files[i], true
	}
	return File{}, false
}

// Source fetches the archive of an exact commit (implemented by the GitHub adapter).
type Source interface {
	Archive(ctx context.Context, userID, repoID, sha string) (io.ReadCloser, error)
}

// Fetch acquires and builds a snapshot for an exact commit.
func Fetch(ctx context.Context, src Source, userID, repoID, ref, sha string, lim Limits) (Snapshot, error) {
	rc, err := src.Archive(ctx, userID, repoID, sha)
	if err != nil {
		return Snapshot{}, err
	}
	defer rc.Close()
	s, err := FromTarGz(rc, lim)
	if err != nil {
		return Snapshot{}, err
	}
	s.RepositoryID, s.Ref, s.Commit = repoID, ref, sha
	return s, nil
}

// FromTarGz reads a GitHub-style tarball (single top-level directory).
func FromTarGz(r io.Reader, lim Limits) (Snapshot, error) {
	counted := &limitedReader{r: r, remaining: lim.MaxArchiveBytes}
	gz, err := gzip.NewReader(counted)
	if err != nil {
		if errors.Is(err, errArchiveLimit) {
			return Snapshot{}, ErrTooLarge
		}
		return Snapshot{}, ErrMalformed
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var snap Snapshot
	var retainedTotal int64
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, errArchiveLimit) {
				return Snapshot{}, ErrTooLarge
			}
			return Snapshot{}, ErrMalformed
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue // GitHub stores the commit SHA here; correlation comes from the request
		}
		rel, ok, err := normalize(hdr.Name)
		if err != nil {
			return Snapshot{}, err
		}
		if !ok {
			continue // top-level directory entry
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
		default:
			snap.Skipped = append(snap.Skipped, rel) // symlinks, hardlinks, devices: recorded, never followed
			continue
		}
		if seen[rel] {
			return Snapshot{}, fmt.Errorf("%w: duplicate entry %q", ErrMalformed, rel)
		}
		seen[rel] = true
		if len(snap.Files) >= lim.MaxFiles {
			return Snapshot{}, ErrTooMany
		}
		if hdr.Size < 0 {
			return Snapshot{}, ErrMalformed
		}
		snap.TotalBytes += hdr.Size
		if snap.TotalBytes > lim.MaxTotalBytes {
			return Snapshot{}, ErrTooLarge
		}

		f := File{Path: rel, Size: hdr.Size}
		head := make([]byte, min64(hdr.Size, 8<<10))
		n, err := io.ReadFull(tr, head)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			if errors.Is(err, errArchiveLimit) {
				return Snapshot{}, ErrTooLarge
			}
			return Snapshot{}, ErrMalformed
		}
		head = head[:n]
		f.Binary = isBinary(head)
		if !f.Binary && hdr.Size <= lim.MaxRetainedBytes && retainedTotal+hdr.Size <= lim.MaxRetainedTotal {
			rest, err := io.ReadAll(io.LimitReader(tr, hdr.Size-int64(n)))
			if err != nil {
				if errors.Is(err, errArchiveLimit) {
					return Snapshot{}, ErrTooLarge
				}
				return Snapshot{}, ErrMalformed
			}
			f.Content = append(head, rest...)
			f.Retained = true
			retainedTotal += int64(len(f.Content))
		}
		// Remaining bytes of non-retained files are discarded by tr.Next().
		snap.Files = append(snap.Files, f)
	}
	if len(snap.Files) == 0 {
		return Snapshot{}, ErrEmpty
	}
	sort.Slice(snap.Files, func(i, j int) bool { return snap.Files[i].Path < snap.Files[j].Path })
	sort.Strings(snap.Skipped)
	return snap, nil
}

// normalize strips the archive's top-level directory and rejects unsafe paths.
// ok=false means the entry is the top-level directory itself.
func normalize(name string) (string, bool, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") || !utf8.ValidString(name) {
		return "", false, fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	if strings.HasPrefix(name, "/") {
		return "", false, fmt.Errorf("%w: absolute path %q", ErrUnsafePath, name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", false, fmt.Errorf("%w: traversal in %q", ErrUnsafePath, name)
		}
	}
	_, rest, found := strings.Cut(strings.TrimPrefix(name, "./"), "/")
	if !found || rest == "" || rest == "/" {
		return "", false, nil
	}
	clean := path.Clean(rest)
	if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", false, fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	return clean, true, nil
}

func isBinary(head []byte) bool {
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	return !utf8.Valid(trimIncompleteRune(head))
}

func trimIncompleteRune(b []byte) []byte {
	for i := 0; i < 3 && len(b) > 0; i++ {
		if utf8.Valid(b) {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

var errArchiveLimit = errors.New("archive byte limit exceeded")

type limitedReader struct {
	r         io.Reader
	remaining int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, errArchiveLimit
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining+1] // read one extra byte to detect overflow
	}
	n, err := l.r.Read(p)
	l.remaining -= int64(n)
	if l.remaining < 0 {
		return n, errArchiveLimit
	}
	return n, err
}
