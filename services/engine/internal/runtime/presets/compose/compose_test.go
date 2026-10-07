package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func golden(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return content
}

func parseGolden(t *testing.T, name string) Document {
	t.Helper()
	doc, err := Parse(golden(t, name))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc
}

func rules(issues []Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Rule)
	}
	return out
}

func hasRule(issues []Issue, rule string) bool {
	for _, i := range issues {
		if i.Rule == rule {
			return true
		}
	}
	return false
}

func severityOf(issues []Issue, rule string) (Severity, bool) {
	for _, i := range issues {
		if i.Rule == rule {
			return i.Severity, true
		}
	}
	return "", false
}

func TestParseValidDocument(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")

	if got := doc.ServiceNames(); len(got) != 2 || got[0] != "api" || got[1] != "db" {
		t.Fatalf("services = %v", got)
	}
	api := doc.Services["api"]
	if api.Build == nil || api.Build.Context != "." || api.Build.Dockerfile != "Dockerfile" {
		t.Fatalf("api build = %+v", api.Build)
	}
	if len(api.DependsOn) != 1 || api.DependsOn[0] != "db" {
		t.Fatalf("api depends_on = %v", api.DependsOn)
	}
	if len(api.Environment) != 2 || api.Environment[0] != "DATABASE_URL" || api.Environment[1] != "NODE_ENV" {
		t.Fatalf("api environment names = %v", api.Environment)
	}
	if len(api.Ports) != 1 || api.Ports[0].Target != "3000" {
		t.Fatalf("api ports = %+v", api.Ports)
	}
	if _, ok := doc.Networks["backend"]; !ok {
		t.Fatal("top-level network backend missing")
	}
	if _, ok := doc.Volumes["dbdata"]; !ok {
		t.Fatal("top-level volume dbdata missing")
	}
}

func TestParseBuildShorthand(t *testing.T) {
	doc, err := Parse([]byte("services:\n  web:\n    build: ./web\n    image: ignored\n"))
	if err != nil {
		t.Fatal(err)
	}
	build := doc.Services["web"].Build
	if build == nil || build.Context != "./web" || build.Dockerfile != "" {
		t.Fatalf("shorthand build = %+v", build)
	}
}

func TestParseDependsOnMappingAndEnvMapping(t *testing.T) {
	content := []byte("services:\n  web:\n    build: .\n    depends_on:\n      db:\n        condition: service_healthy\n    environment:\n      ZED: 1\n      ALPHA: 2\n")
	doc, err := Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	web := doc.Services["web"]
	if len(web.DependsOn) != 1 || web.DependsOn[0] != "db" {
		t.Fatalf("depends_on = %v", web.DependsOn)
	}
	if len(web.Environment) != 2 || web.Environment[0] != "ALPHA" || web.Environment[1] != "ZED" {
		t.Fatalf("environment = %v", web.Environment)
	}
}

func TestParsePortForms(t *testing.T) {
	content := []byte("services:\n  web:\n    image: nginx\n    ports:\n      - \"3000\"\n      - \"8080:3000\"\n      - \"127.0.0.1:80:8080/tcp\"\n      - target: 5000\n        published: 5001\n        host_ip: 0.0.0.0\n")
	doc, err := Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	ports := doc.Services["web"].Ports
	if len(ports) != 4 {
		t.Fatalf("ports = %+v", ports)
	}
	if ports[0].Target != "3000" || ports[0].Published != "" {
		t.Fatalf("bare container port = %+v", ports[0])
	}
	if ports[1].Published != "8080" || ports[1].Target != "3000" {
		t.Fatalf("host:container = %+v", ports[1])
	}
	if ports[2].HostIP != "127.0.0.1" || ports[2].Published != "80" || ports[2].Target != "8080" || ports[2].Protocol != "tcp" {
		t.Fatalf("ip:host:container = %+v", ports[2])
	}
	if ports[3].HostIP != "0.0.0.0" || ports[3].HostPort() != 5001 {
		t.Fatalf("long form = %+v", ports[3])
	}
}

func TestParseRejectsInvalidYAML(t *testing.T) {
	if _, err := Parse([]byte("services: [::")); err == nil {
		t.Fatal("invalid YAML must be rejected")
	}
}

func TestValidateCleanDocument(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")
	issues := doc.Validate()
	// The api service publishes "3000" on all interfaces, which is only an
	// info at the structural level (it is the public service).
	for _, i := range issues {
		if i.Severity == SeverityError {
			t.Fatalf("clean document has a blocking issue: %+v", i)
		}
	}
}

func TestValidateHostMount(t *testing.T) {
	doc := parseGolden(t, "host-mount.yaml")
	issues := doc.Validate()
	if !hasRule(issues, RuleHostMountSensitive) {
		t.Fatalf("rules = %v", rules(issues))
	}
	if sev, _ := severityOf(issues, RuleHostMountSensitive); sev != SeverityError {
		t.Fatalf("host mount severity = %s", sev)
	}
}

func TestValidatePrivileged(t *testing.T) {
	doc := parseGolden(t, "privileged.yaml")
	if sev, ok := severityOf(doc.Validate(), RulePrivileged); !ok || sev != SeverityError {
		t.Fatalf("privileged rules = %v", rules(doc.Validate()))
	}
}

func TestValidateHostNamespaces(t *testing.T) {
	doc := parseGolden(t, "host-network.yaml")
	issues := doc.Validate()
	for _, rule := range []string{RuleNetworkModeHost, RulePIDHost, RuleIPCHost} {
		if sev, ok := severityOf(issues, rule); !ok || sev != SeverityError {
			t.Fatalf("rule %s missing or not error: %v", rule, rules(issues))
		}
	}
}

func TestValidateLowPort(t *testing.T) {
	doc := parseGolden(t, "low-port.yaml")
	if sev, ok := severityOf(doc.Validate(), RulePrivilegedPort); !ok || sev != SeverityError {
		t.Fatalf("low port rules = %v", rules(doc.Validate()))
	}
}

func TestValidateVolumesFromWarns(t *testing.T) {
	doc := parseGolden(t, "volumes-from.yaml")
	if sev, ok := severityOf(doc.Validate(), RuleVolumesFrom); !ok || sev != SeverityWarn {
		t.Fatalf("volumes_from rules = %v", rules(doc.Validate()))
	}
}

func TestValidateNoServices(t *testing.T) {
	doc, err := Parse([]byte("version: \"3\"\nservices: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sev, ok := severityOf(doc.Validate(), RuleNoServices); !ok || sev != SeverityError {
		t.Fatalf("rules = %v", rules(doc.Validate()))
	}
}
