// Package docker provides the generic Docker runtime preset (issue #122):
// validation of repository Dockerfiles for Axiom and port discovery from
// EXPOSE instructions.
//
// Scope (#122):
//
//   - Dockerfile detection and loading from a repository snapshot.
//   - Validation rules with stable rule codes and severities so the
//     analyzer/profile layer can surface them without parsing messages.
//   - Port discovery from EXPOSE instructions.
//
// Design decisions (documented per #122):
//
//   - {@link FileAccessor} is a deliberately local interface
//     ({@code Read(path) ([]byte, bool)}). The package never imports the
//     analyzer, snapshot or storage layers: the caller adapts whatever
//     snapshot source it has. This keeps the preset pure and unit-testable.
//   - Validation never blocks on style. Only two rules are errors
//     (missing FROM, remote ADD); everything else is a warning or info so a
//     valid Dockerfile project reaches the Deployment Plan and runtime
//     without source rewriting.
//   - Port discovery reads EXPOSE only. There is no manifest/axiom.yaml
//     concept in this package: an explicit manifest port override wins over
//     the preferred EXPOSE port, and the profile layer decides that
//     precedence. {@link DiscoverPorts} returns the raw facts
//     ({@code Ports}, {@code Preferred}, {@code HasCMD}) and nothing more.
//   - Multi-stage Dockerfiles are fully supported: FROM may appear several
//     times, ARG before FROM is allowed, and line continuations plus
//     comments are handled by the parser.
package docker

import "strings"

// DefaultDockerfilePath is the conventional Dockerfile location.
const DefaultDockerfilePath = "Dockerfile"

// FileAccessor reads repository files from a snapshot. It is defined locally
// (not imported from the analyzer/snapshot packages) so this preset stays
// decoupled from how snapshots are produced and stored.
type FileAccessor interface {
	// Read returns the file content and true when the path exists.
	Read(path string) ([]byte, bool)
}

// SnapshotFile is a single file of a repository snapshot.
type SnapshotFile struct {
	Path    string
	Content []byte
}

// SnapshotFiles adapts a slice of snapshot files to {@link FileAccessor}.
// Lookup is a linear scan; snapshots are small (bounded read-only source).
type SnapshotFiles []SnapshotFile

// Read implements {@link FileAccessor}.
func (files SnapshotFiles) Read(path string) ([]byte, bool) {
	normalized := normalizePath(path)
	for _, file := range files {
		if normalizePath(file.Path) == normalized {
			return file.Content, true
		}
	}
	return nil, false
}

// normalizePath trims a leading "./" so "Dockerfile" and "./Dockerfile"
// resolve to the same file.
func normalizePath(path string) string {
	return strings.TrimPrefix(strings.TrimSpace(path), "./")
}

// resolvePath returns the Dockerfile path to use, defaulting to
// {@link DefaultDockerfilePath} when empty.
func resolvePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return DefaultDockerfilePath
	}
	return normalizePath(path)
}

// DockerfileExists reports whether the snapshot contains a Dockerfile at the
// given path (default "Dockerfile").
func DockerfileExists(files FileAccessor, path string) bool {
	if files == nil {
		return false
	}
	_, ok := files.Read(resolvePath(path))
	return ok
}

// LoadDockerfile reads the Dockerfile content from the snapshot. The second
// return value is false when the file does not exist.
func LoadDockerfile(files FileAccessor, path string) ([]byte, bool) {
	if files == nil {
		return nil, false
	}
	return files.Read(resolvePath(path))
}
