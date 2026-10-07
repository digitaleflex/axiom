package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolverResolvesRequiredNames(t *testing.T) {
	store, _, ctx := newTestStore(t)
	if err := store.Put(ctx, "application:app_1", "DATABASE_URL", "postgres://db/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "application:app_1", "API_KEY", "gho_api_secret"); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Store: &store}
	env, err := r.ResolveRequired(ctx, "application:app_1", []string{"DATABASE_URL", "API_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 || env[0] != "API_KEY=gho_api_secret" || env[1] != "DATABASE_URL=postgres://db/app" {
		t.Fatalf("env = %v", env)
	}
	if len(r.Touched) != 2 || r.Touched[0] != "API_KEY" || r.Touched[1] != "DATABASE_URL" {
		t.Fatalf("touched = %v", r.Touched)
	}
}

func TestResolverMissingRequiredListsNamesOnly(t *testing.T) {
	store, _, ctx := newTestStore(t)
	if err := store.Put(ctx, "application:app_1", "DATABASE_URL", "postgres://db/app"); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Store: &store}
	_, err := r.ResolveRequired(ctx, "application:app_1", []string{"DATABASE_URL", "API_KEY", "STRIPE_KEY"})
	if err == nil {
		t.Fatal("expected MissingRequiredError")
	}
	var missing *MissingRequiredError
	if !errors.As(err, &missing) {
		t.Fatalf("error type = %T, want *MissingRequiredError", err)
	}
	if len(missing.Missing) != 2 || missing.Missing[0] != "API_KEY" || missing.Missing[1] != "STRIPE_KEY" {
		t.Fatalf("missing = %v", missing.Missing)
	}
	// The error message lists names only — never values.
	if !strings.Contains(err.Error(), "API_KEY") || !strings.Contains(err.Error(), "STRIPE_KEY") {
		t.Fatalf("error must list missing names: %v", err)
	}
	if strings.Contains(err.Error(), "postgres://db/app") || strings.Contains(err.Error(), "gho_api_secret") {
		t.Fatalf("error leaks values: %v", err)
	}
	// Touched records only the names that resolved.
	if len(r.Touched) != 1 || r.Touched[0] != "DATABASE_URL" {
		t.Fatalf("touched = %v", r.Touched)
	}
}

func TestResolverOptionalSkipsMissing(t *testing.T) {
	store, _, ctx := newTestStore(t)
	if err := store.Put(ctx, "application:app_1", "DATABASE_URL", "postgres://db/app"); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Store: &store}
	env, err := r.Resolve(ctx, "application:app_1", []string{"DATABASE_URL", "API_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 1 || env[0] != "DATABASE_URL=postgres://db/app" {
		t.Fatalf("env = %v", env)
	}
	if len(r.Touched) != 1 || r.Touched[0] != "DATABASE_URL" {
		t.Fatalf("touched = %v", r.Touched)
	}
}

func TestResolverDeterministicOrder(t *testing.T) {
	store, _, ctx := newTestStore(t)
	for _, n := range []string{"ZED", "ALPHA", "MIDDLE"} {
		if err := store.Put(ctx, "application:app_1", n, "v_"+strings.ToLower(n)); err != nil {
			t.Fatal(err)
		}
	}
	r := &Resolver{Store: &store}
	env, err := r.Resolve(ctx, "application:app_1", []string{"ZED", "MIDDLE", "ALPHA"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ALPHA=v_alpha", "MIDDLE=v_middle", "ZED=v_zed"}
	if len(env) != len(want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Fatalf("env = %v, want %v", env, want)
		}
	}
	// Re-resolving with shuffled input yields the same order.
	r2 := &Resolver{Store: &store}
	env2, err := r2.Resolve(ctx, "application:app_1", []string{"ALPHA", "ZED", "MIDDLE"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if env2[i] != want[i] {
			t.Fatalf("env2 = %v, want %v", env2, want)
		}
	}
}

func TestResolverRequiresStore(t *testing.T) {
	r := &Resolver{}
	if _, err := r.ResolveRequired(context.Background(), "application:app_1", []string{"X"}); err == nil {
		t.Fatal("expected error when store is not configured")
	}
}

func TestRedactForOutput(t *testing.T) {
	in := []string{"DATABASE_URL=postgres://db/app", "API_KEY=gho_secret", "EMPTY="}
	out := RedactForOutput(in)
	want := []string{"DATABASE_URL=***", "API_KEY=***", "EMPTY=***"}
	if len(out) != len(want) {
		t.Fatalf("out = %v, want %v", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("out = %v, want %v", out, want)
		}
	}
	// The input slice is not mutated.
	if in[0] != "DATABASE_URL=postgres://db/app" {
		t.Fatalf("input mutated: %v", in)
	}
	// Entries without "=" are redacted wholesale.
	if got := RedactForOutput([]string{"bare"}); len(got) != 1 || got[0] != RedactionPlaceholder {
		t.Fatalf("bare entry = %v", got)
	}
}

func TestMissingRequiredErrorMessage(t *testing.T) {
	err := &MissingRequiredError{Missing: []string{"API_KEY", "STRIPE_KEY"}}
	msg := err.Error()
	if !strings.Contains(msg, "API_KEY") || !strings.Contains(msg, "STRIPE_KEY") {
		t.Fatalf("message must list names: %q", msg)
	}
}
