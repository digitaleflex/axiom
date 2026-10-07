package ownership

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{
		"axiom",
		"a",
		"axiom-my-app-123",
		"srv-eu-1",
		strings.Repeat("a", 63),
		"0",
		"a-b-c",
		"axiom--x", // interior double hyphen is allowed by the charset rules
	}
	for _, name := range valid {
		if !ValidateName(name) {
			t.Errorf("ValidateName(%q) = false, want true", name)
		}
	}
	invalid := []string{
		"",
		"A",                     // uppercase
		"-abc",                  // leading hyphen
		"abc-",                  // trailing hyphen
		"ab_cd",                 // underscore
		"ab.cd",                 // dot
		strings.Repeat("a", 64), // too long
		" leading",              // space
		"trailing ",             // space
	}
	for _, name := range invalid {
		if ValidateName(name) {
			t.Errorf("ValidateName(%q) = true, want false", name)
		}
	}
}

func TestContainerName(t *testing.T) {
	name, err := ContainerName("my-app", "abcd1234")
	if err != nil {
		t.Fatalf("ContainerName(valid): %v", err)
	}
	if name != "axiom-my-app-abcd1234" {
		t.Errorf("ContainerName = %q, want axiom-my-app-abcd1234", name)
	}
	if !ValidateName(name) {
		t.Errorf("ContainerName result %q must pass ValidateName", name)
	}

	slugs := []string{"", "My-App", "-lead", "trail_", "has space", strings.Repeat("s", 62)}
	for _, slug := range slugs {
		if _, err := ContainerName(slug, "abcd1234"); err == nil {
			t.Errorf("ContainerName(slug %q) = nil error, want ErrInvalidName", slug)
		} else if !errors.Is(err, ErrInvalidName) {
			t.Errorf("ContainerName(slug %q) error %v must wrap ErrInvalidName", slug, err)
		}
	}

	// A valid slug that pushes the total past 63 chars must be rejected.
	if _, err := ContainerName(strings.Repeat("s", 50), "abcd1234"); err == nil {
		t.Error("ContainerName(oversized) = nil error, want ErrInvalidName")
	}
}

func TestNetworkName(t *testing.T) {
	if got := NetworkName("srv-eu-1"); got != "axiom-srv-eu-1-net" {
		t.Errorf("NetworkName = %q, want axiom-srv-eu-1-net", got)
	}
}

func TestIsManaged(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"managed with deployment", map[string]string{LabelManaged: "true", LabelDeployment: "dep_1"}, true},
		{"managed without deployment", map[string]string{LabelManaged: "true"}, false},
		{"deployment only", map[string]string{LabelDeployment: "dep_1"}, false},
		{"managed false with deployment", map[string]string{LabelManaged: "false", LabelDeployment: "dep_1"}, false},
		{"managed TRUE (case-sensitive)", map[string]string{LabelManaged: "TRUE", LabelDeployment: "dep_1"}, false},
		{"empty labels", map[string]string{}, false},
		{"nil labels", nil, false},
		{"managed with empty deployment", map[string]string{LabelManaged: "true", LabelDeployment: ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsManaged(tt.labels); got != tt.want {
				t.Errorf("IsManaged(%v) = %v, want %v", tt.labels, got, tt.want)
			}
		})
	}
}

func TestAssertContainer(t *testing.T) {
	dep := "dep_" + strings.Repeat("a", 24)
	managed := NewLabels(dep, "app_1", "srv_1")

	if err := AssertContainer("axiom-my-app-abcd1234", managed, dep); err != nil {
		t.Fatalf("AssertContainer(managed, in scope): %v", err)
	}

	err := AssertContainer("axiom-my-app-abcd1234", map[string]string{}, dep)
	if !errors.Is(err, ErrNotManaged) || !errors.Is(err, ErrForeignResource) {
		t.Errorf("AssertContainer(unmanaged) = %v, want ErrNotManaged wrapping ErrForeignResource", err)
	}

	err = AssertContainer("axiom-my-app-abcd1234", managed, "dep_"+strings.Repeat("b", 24))
	if !errors.Is(err, ErrWrongDeployment) || !errors.Is(err, ErrForeignResource) {
		t.Errorf("AssertContainer(wrong deployment) = %v, want ErrWrongDeployment wrapping ErrForeignResource", err)
	}

	err = AssertContainer("Not_A_Name", managed, dep)
	if !errors.Is(err, ErrInvalidName) {
		t.Errorf("AssertContainer(bad name) = %v, want ErrInvalidName", err)
	}
}

func TestAssertNetwork(t *testing.T) {
	dep := "dep_" + strings.Repeat("a", 24)
	managed := NewLabels(dep, "app_1", "srv_1")

	if err := AssertNetwork(NetworkName("srv_1"), managed, dep); err != nil {
		t.Fatalf("AssertNetwork(managed, in scope): %v", err)
	}

	err := AssertNetwork(NetworkName("srv_1"), map[string]string{}, dep)
	if !errors.Is(err, ErrNotManaged) {
		t.Errorf("AssertNetwork(unmanaged) = %v, want ErrNotManaged", err)
	}

	err = AssertNetwork(NetworkName("srv_1"), managed, "dep_"+strings.Repeat("b", 24))
	if !errors.Is(err, ErrWrongDeployment) {
		t.Errorf("AssertNetwork(wrong deployment) = %v, want ErrWrongDeployment", err)
	}

	err = AssertNetwork("user-network", managed, dep)
	if !errors.Is(err, ErrInvalidName) {
		t.Errorf("AssertNetwork(foreign name) = %v, want ErrInvalidName", err)
	}
}

func TestCleanupScope(t *testing.T) {
	depA := "dep_" + strings.Repeat("a", 24)
	depB := "dep_" + strings.Repeat("b", 24)
	scope := CleanupScope{DeploymentID: depA}

	resources := []Resource{
		{Name: "axiom-a-11111111", Labels: NewLabels(depA, "app_1", "srv_1")},
		{Name: "axiom-b-22222222", Labels: NewLabels(depB, "app_2", "srv_1")},
		{Name: "user-container", Labels: map[string]string{}},
		{Name: "axiom-c-33333333", Labels: map[string]string{LabelManaged: "true"}}, // no deployment
		{Name: "axiom-d-44444444", Labels: NewLabels(depA, "app_3", "srv_1")},
	}

	removable := scope.Removable(resources)
	if len(removable) != 2 {
		t.Fatalf("Removable returned %d resources, want 2", len(removable))
	}
	if removable[0].Name != "axiom-a-11111111" || removable[1].Name != "axiom-d-44444444" {
		t.Errorf("Removable = [%q %q], want [axiom-a-11111111 axiom-d-44444444]", removable[0].Name, removable[1].Name)
	}

	if scope.MayRemove("user-container", map[string]string{}) {
		t.Error("MayRemove(foreign) = true, want false")
	}
	if !scope.MayRemove("axiom-a-11111111", NewLabels(depA, "app_1", "srv_1")) {
		t.Error("MayRemove(managed, in scope) = false, want true")
	}
}
