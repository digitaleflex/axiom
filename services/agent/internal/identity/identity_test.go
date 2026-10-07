package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstRunGeneratesUnregisteredIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	s := NewStore(path)

	id, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ValidID(id.AgentID) {
		t.Fatalf("generated agent id = %q, want agent_<24hex>", id.AgentID)
	}
	if id.Registered || id.ServerID != "" {
		t.Fatalf("first run must be unregistered: %+v", id)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("identity file not persisted: %v", err)
	}
}

func TestIdentityPersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	first, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	second, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if first.AgentID != second.AgentID {
		t.Fatalf("agent id changed across reload: %q != %q", first.AgentID, second.AgentID)
	}
}

func TestSaveBindsServerAndPersistsRegistered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	s := NewStore(path)
	id, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	id.ServerID = "srv_eu_1"
	id.Registered = true
	if err := s.Save(id); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.Registered || reloaded.ServerID != "srv_eu_1" || reloaded.AgentID != id.AgentID {
		t.Fatalf("reloaded = %+v, want registered binding to srv_eu_1", reloaded)
	}
}

func TestIdentityFileIs0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if _, err := NewStore(path).Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("identity file perm = %o, want 600", perm)
	}
}

func TestSaveRejectsMalformedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	s := NewStore(path)
	if err := s.Save(Identity{AgentID: "not-an-agent-id"}); err == nil {
		t.Fatal("malformed agent id must be rejected")
	}
	if err := s.Save(Identity{AgentID: GenerateID(), Registered: true}); err == nil {
		t.Fatal("registered identity without server must be rejected")
	}
}

func TestLoadRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).Load(); err == nil {
		t.Fatal("corrupt identity file must be an error, never a silent rotation")
	}
}

func TestGenerateIDUniquenessAndFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		id := GenerateID()
		if !strings.HasPrefix(id, "agent_") || len(id) != len("agent_")+24 {
			t.Fatalf("bad id format: %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id: %q", id)
		}
		seen[id] = true
	}
}
