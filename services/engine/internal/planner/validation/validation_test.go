package validation

import (
	"strings"
	"testing"
)

func valid() Input {
	return Input{SchemaVersion: 1, ApplicationID: "app_1", ProfileVersion: 1, Commit: strings.Repeat("b", 40), ServerID: "srv_1",
		ServerCapabilities: []string{"docker", "traefik"}, Environment: "production", Preset: "nextjs", BuildStrategy: "source",
		BuildCommand: "pnpm run build", StartCommand: "pnpm start", Port: 3000, Domain: "app.example.com", HealthType: "http",
		HealthPath: "/", HealthTimeout: 5, HealthRetries: 20, RollbackStrategy: "keep_previous_until_verified",
		Steps: []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"}, Configuration: []string{"DATABASE_URL"}}
}

func fields(errs Errors) string {
	var out []string
	for _, e := range errs {
		out = append(out, e.Field)
	}
	return strings.Join(out, ",")
}

func TestValidPlan(t *testing.T) {
	if errs := Validate(valid()); errs != nil {
		t.Fatalf("valid plan rejected: %v", errs)
	}
}

func TestMissingAndInvalidFields(t *testing.T) {
	cases := map[string]func(*Input){
		"applicationId":             func(i *Input) { i.ApplicationID = "" },
		"applicationProfileVersion": func(i *Input) { i.ProfileVersion = 0 },
		"source.commit":             func(i *Input) { i.Commit = "abc" },
		"serverId":                  func(i *Input) { i.ServerID = "" },
		"environment":               func(i *Input) { i.Environment = "dev" },
		"strategy":                  func(i *Input) { i.Preset = "kubernetes" },
		"build.strategy":            func(i *Input) { i.BuildStrategy = "dockerfile" },
		"runtime.port":              func(i *Input) { i.Port = 70000 },
		"network.domain":            func(i *Input) { i.Domain = "http://x.com/" },
		"healthCheck.path":          func(i *Input) { i.HealthPath = "health" },
		"healthCheck.type":          func(i *Input) { i.HealthType = "grpc" },
		"healthCheck":               func(i *Input) { i.HealthRetries = 0 },
		"rollback.strategy":         func(i *Input) { i.RollbackStrategy = "" },
		"build.command":             func(i *Input) { i.BuildCommand = "echo $(id)" },
		"runtime.configuration":     func(i *Input) { i.Configuration = []string{"bad-name"} },
	}
	for field, mutate := range cases {
		in := valid()
		mutate(&in)
		if got := fields(Validate(in)); !strings.Contains(got, field) {
			t.Errorf("%s: got errors %q", field, got)
		}
	}
}

func TestStepRules(t *testing.T) {
	cases := map[string][]string{
		"unknown":   {"PREPARE", "BUILD", "CREATE_RUNTIME", "START", "VERIFY"},
		"duplicate": {"BUILD", "BUILD", "CREATE_RUNTIME", "START", "VERIFY"},
		"order":     {"CREATE_RUNTIME", "BUILD", "START", "VERIFY"},
		"missing":   {"BUILD", "CREATE_RUNTIME", "START"},
		"empty":     {},
	}
	for name, steps := range cases {
		in := valid()
		in.Steps = steps
		if errs := Validate(in); len(errs) == 0 {
			t.Errorf("%s steps accepted", name)
		}
	}
	in := valid()
	in.Steps = []string{"CREATE_RUNTIME", "NETWORK", "START", "VERIFY"} // no build (image-only) is allowed
	if errs := Validate(in); errs != nil {
		t.Errorf("subset in canonical order rejected: %v", errs)
	}
}

func TestCapabilities(t *testing.T) {
	in := valid()
	in.Preset, in.BuildStrategy = "compose", "compose"
	in.StartCommand = ""
	if got := fields(Validate(in)); got != "serverId" {
		t.Errorf("compose without docker_compose: %q", got)
	}
}
