// Package workspace provides isolated, ephemeral build workspaces (issue #98).
// Each build gets a unique directory (0700) under a configured root; source
// archives are extracted with traversal protection, file/size limits and no
// symlinks. Cleanup is deterministic and idempotent. Workspaces never receive
// control-plane credentials.
package workspace

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
)

var (
	ErrTooLarge   = errors.New("workspace: source exceeds size limits")
	ErrTooMany    = errors.New("workspace: source has too many files")
	ErrMalformed  = errors.New("workspace: malformed archive")
	ErrUnsafePath = errors.New("workspace: unsafe path in archive")
	ErrEmpty      = errors.New("workspace: archive contains no files")
)

// Limits bound extraction resource usage.
type Limits struct {
	MaxArchiveBytes int64 // compressed bytes read
	MaxTotalBytes   int64 // sum of file sizes
	MaxFiles        int
	MaxFileBytes    int64 // largest single file
}

// DefaultLimits suit V0.1 application repositories.
var DefaultLimits = Limits{
	MaxArchiveBytes: 200 << 20,
	MaxTotalBytes:   1 << 30,
	MaxFiles:        50_000,
	MaxFileBytes:    100 << 20,
}

// Manager creates workspaces under Root.
type Manager struct {
	Root   string
	Limits Limits
}

// Workspace is one isolated build directory.
type Workspace struct {
	Dir     string
	cleaned atomic.Bool
}

// Create makes a unique workspace directory (0700). The caller must call
// Cleanup; it also runs on extraction failure.
func (m *Manager) Create(ctx context.Context) (*Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lim := m.Limits
	if lim.MaxFiles == 0 {
		lim = DefaultLimits
	}
	if m.Root == "" {
		return nil, errors.New("workspace: root directory is required")
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	dir := filepath.Join(m.Root, "axiom-build-"+hex.EncodeToString(b))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: create directory: %w", err)
	}
	return &Workspace{Dir: dir}, nil
}

// Cleanup removes the workspace directory. It is idempotent: repeated calls
// return nil. A failure is returned so the caller can alert, not ignore it.
func (w *Workspace) Cleanup() error {
	if w.cleaned.Swap(true) {
		return nil
	}
	if err := os.RemoveAll(w.Dir); err != nil {
		return fmt.Errorf("workspace: cleanup %s: %w", w.Dir, err)
	}
	return nil
}

// ExtractTarGz streams a gzipped tar archive into the workspace.
// On any error the workspace is cleaned up before returning.
func (w *Workspace) ExtractTarGz(ctx context.Context, r io.Reader, lim Limits) (skipped []string, err error) {
	if lim.MaxFiles == 0 {
		lim = DefaultLimits
	}
	defer func() {
		if err != nil {
			_ = w.Cleanup()
		}
	}()
	counted := &limitedReader{r: r, remaining: lim.MaxArchiveBytes}
	gz, err := gzip.NewReader(counted)
	if err != nil {
		if errors.Is(err, errLimit) {
			return nil, ErrTooLarge
		}
		return nil, ErrMalformed
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var files int
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, errLimit) {
				return nil, ErrTooLarge
			}
			return nil, ErrMalformed
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		rel, isDir, err := cleanPath(hdr.Name)
		if err != nil {
			return nil, err
		}
		target := filepath.Join(w.Dir, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, fmt.Errorf("workspace: mkdir: %w", err)
			}
			continue
		case tar.TypeReg, tar.TypeRegA:
		default:
			skipped = append(skipped, rel) // symlinks, hardlinks, devices: never followed
			continue
		}
		if isDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, fmt.Errorf("workspace: mkdir: %w", err)
			}
			continue
		}
		if hdr.Size < 0 || hdr.Size > lim.MaxFileBytes {
			return nil, ErrTooLarge
		}
		files++
		if files > lim.MaxFiles {
			return nil, ErrTooMany
		}
		total += hdr.Size
		if total > lim.MaxTotalBytes {
			return nil, ErrTooLarge
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("workspace: mkdir: %w", err)
		}
		// Regular files get 0644: setuid/setgid/executable bits from the
		// archive are never honored. O_EXCL refuses pre-existing paths.
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return nil, fmt.Errorf("workspace: create file: %w", err)
		}
		_, copyErr := io.CopyN(f, &cancellableReader{ctx: ctx, r: tr}, hdr.Size)
		closeErr := f.Close()
		if copyErr != nil {
			if errors.Is(copyErr, errLimit) || ctx.Err() != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, ErrTooLarge
			}
			return nil, ErrMalformed
		}
		if closeErr != nil {
			return nil, fmt.Errorf("workspace: write file: %w", closeErr)
		}
	}
	if files == 0 {
		return nil, ErrEmpty
	}
	return skipped, nil
}

// cleanPath strips the archive top-level directory and rejects unsafe paths.
func cleanPath(name string) (rel string, isDir bool, err error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") {
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
	trimmed := strings.TrimSuffix(strings.TrimPrefix(name, "./"), "/")
	isDir = strings.HasSuffix(name, "/")
	top, rest, found := strings.Cut(trimmed, "/")
	if !found || rest == "" || top == "" {
		return "", true, nil // top-level entry itself
	}
	clean := path.Clean(rest)
	if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", false, fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	return clean, false, nil
}

var errLimit = errors.New("byte limit exceeded")

type limitedReader struct {
	r         io.Reader
	remaining int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, errLimit
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining+1]
	}
	n, err := l.r.Read(p)
	l.remaining -= int64(n)
	if l.remaining < 0 {
		return n, errLimit
	}
	return n, err
}

type cancellableReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *cancellableReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
