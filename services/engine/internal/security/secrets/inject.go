package secrets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// RedactionPlaceholder replaces every env value in RedactForOutput. It mirrors
// logs.RedactionMarker in shape but applies to KEY=VALUE entries.
const RedactionPlaceholder = "***"

// MissingRequiredError is returned when one or more required configuration
// names have no stored value. It lists names only — never values.
type MissingRequiredError struct {
	Missing []string
}

func (e *MissingRequiredError) Error() string {
	return fmt.Sprintf("secrets: missing required configuration: %s", strings.Join(e.Missing, ", "))
}

// Resolver resolves configuration NAMES for a scope into runtime environment
// entries (KEY=VALUE) at the injection boundary. Touched records which names
// resolved, in deterministic (sorted) order.
type Resolver struct {
	Store   *EncryptedStore
	Touched []string
}

// Resolve resolves names for scope into KEY=VALUE entries with deterministic
// (sorted) order. Names with no stored value are skipped; Touched lists the
// names that resolved.
func (r *Resolver) Resolve(ctx context.Context, scope string, names []string) ([]string, error) {
	return r.resolve(ctx, scope, names, false)
}

// ResolveRequired is Resolve, but fails with *MissingRequiredError when any
// required name has no stored value. The error lists the missing names only.
func (r *Resolver) ResolveRequired(ctx context.Context, scope string, required []string) ([]string, error) {
	return r.resolve(ctx, scope, required, true)
}

func (r *Resolver) resolve(ctx context.Context, scope string, names []string, required bool) ([]string, error) {
	if r.Store == nil {
		return nil, errors.New("secrets: resolver store is not configured")
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	env := make([]string, 0, len(sorted))
	var missing, touched []string
	for _, name := range sorted {
		v, err := r.Store.Get(ctx, scope, name)
		if errors.Is(err, ErrNotFound) {
			if required && (len(missing) == 0 || missing[len(missing)-1] != name) {
				missing = append(missing, name)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		env = append(env, name+"="+v)
		touched = append(touched, name)
	}
	r.Touched = touched
	if len(missing) > 0 {
		return nil, &MissingRequiredError{Missing: missing}
	}
	return env, nil
}

// RedactForOutput returns a copy of env entries with every value replaced by
// "***", preserving keys. Entries without "=" are redacted wholesale. Use it
// before logging or otherwise exposing env entries.
func RedactForOutput(values []string) []string {
	out := make([]string, len(values))
	for i, kv := range values {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			out[i] = RedactionPlaceholder
			continue
		}
		out[i] = k + "=" + RedactionPlaceholder
	}
	return out
}
