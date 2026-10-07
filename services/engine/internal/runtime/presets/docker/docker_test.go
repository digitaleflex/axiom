package docker

import (
	"testing"
)

func snapshot(files ...SnapshotFile) SnapshotFiles {
	return SnapshotFiles(files)
}

func TestSnapshotFilesRead(t *testing.T) {
	files := snapshot(
		SnapshotFile{Path: "Dockerfile", Content: []byte("FROM node:20-alpine\n")},
		SnapshotFile{Path: "package.json", Content: []byte("{}")},
	)

	if _, ok := files.Read("Dockerfile"); !ok {
		t.Fatal("Dockerfile should be readable")
	}
	if _, ok := files.Read("./Dockerfile"); !ok {
		t.Fatal("./Dockerfile should normalize to Dockerfile")
	}
	if _, ok := files.Read("missing.txt"); ok {
		t.Fatal("missing file must not be readable")
	}
}

func TestDockerfileExists(t *testing.T) {
	files := snapshot(SnapshotFile{Path: "Dockerfile", Content: []byte("FROM x\n")})

	if !DockerfileExists(files, "") {
		t.Fatal("default path should find Dockerfile")
	}
	if !DockerfileExists(files, "Dockerfile") {
		t.Fatal("explicit path should find Dockerfile")
	}
	if DockerfileExists(files, "docker-compose.yml") {
		t.Fatal("other paths must not match")
	}
	if DockerfileExists(nil, "") {
		t.Fatal("nil accessor must not panic and report false")
	}
}

func TestLoadDockerfile(t *testing.T) {
	content := []byte("FROM node:20-alpine\nEXPOSE 3000\n")
	files := snapshot(SnapshotFile{Path: "Dockerfile", Content: content})

	loaded, ok := LoadDockerfile(files, "")
	if !ok {
		t.Fatal("load should succeed")
	}
	if string(loaded) != string(content) {
		t.Fatalf("loaded = %q", loaded)
	}

	if _, ok := LoadDockerfile(files, "Containerfile"); ok {
		t.Fatal("missing file must report false")
	}
	if _, ok := LoadDockerfile(nil, ""); ok {
		t.Fatal("nil accessor must report false")
	}
}
