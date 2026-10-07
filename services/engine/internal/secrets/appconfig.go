// Package appconfig is the application configuration service (issue #126).
// It stores configuration values at rest, encrypted and scoped per
// application, and resolves them into runtime environment entries at the
// injection boundary. Values are write-only: List returns metadata only,
// never plaintext.
package appconfig

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/security/secrets"
)

// ScopePrefix namespaces every application configuration value in the
// secret store (security/secrets.EncryptedStore).
const ScopePrefix = "application:"

// scopeFor returns the store scope of one application.
func scopeFor(appID string) string { return ScopePrefix + appID }

// nameRe matches valid configuration names (env-var style, matching the
// planner's runtime.configuration contract).
var nameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ValidName reports whether name is an acceptable configuration name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Entry is the write-only view of one configuration value: metadata only,
// never the value itself.
type Entry struct {
	Name      string    `json:"name"`
	Secret    bool      `json:"secret"`
	IsSet     bool      `json:"isSet"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Service stores application configuration values at rest, encrypted, and
// resolves them into runtime environment entries. Values are write-only:
// List never returns plaintext.
type Service struct {
	Store *secrets.EncryptedStore
}

// Set validates name and stores value sealed under scope
// application:<appID>. The secret flag is accepted for forward
// compatibility: every value in the store is encrypted at rest and
// write-only, so all entries report Secret = true.
func (s *Service) Set(ctx context.Context, appID, name, value string, secret bool) error {
	if !ValidName(name) {
		return fmt.Errorf("appconfig: invalid configuration name %q", name)
	}
	return s.Store.Put(ctx, scopeFor(appID), name, value)
}

// List returns metadata for every stored configuration value of the app,
// ordered by name. Values are never included.
func (s *Service) List(ctx context.Context, appID string) ([]Entry, error) {
	metas, err := s.Store.ListNames(ctx, scopeFor(appID))
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(metas))
	for _, m := range metas {
		out = append(out, Entry{Name: m.Name, Secret: true, IsSet: true, UpdatedAt: m.UpdatedAt})
	}
	return out, nil
}

// Delete removes one configuration value of the app.
func (s *Service) Delete(ctx context.Context, appID, name string) error {
	return s.Store.Delete(ctx, scopeFor(appID), name)
}

// Resolve resolves required configuration names into KEY=VALUE env entries
// for injection into the build/runtime boundary. Missing names produce a
// *secrets.MissingRequiredError listing names only.
func (s *Service) Resolve(ctx context.Context, appID string, required []string) ([]string, error) {
	r := &secrets.Resolver{Store: s.Store}
	return r.ResolveRequired(ctx, scopeFor(appID), required)
}
