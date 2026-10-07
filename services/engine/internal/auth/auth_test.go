package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

const goodPassword = "correct horse battery staple"

func newTestService(clock *time.Time) *Service {
	return NewService(NewMemoryStore(),
		WithPasswordIterations(1000),
		WithClock(func() time.Time { return *clock }),
		WithTouchInterval(0),
	)
}

func TestRegisterValidation(t *testing.T) {
	clock := time.Now().UTC()
	svc := newTestService(&clock)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "not-an-email", goodPassword, ""); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("invalid email err = %v, want ErrInvalidEmail", err)
	}
	if _, err := svc.Register(ctx, "jane@example.com", "short", ""); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password err = %v, want ErrWeakPassword", err)
	}

	user, err := svc.Register(ctx, "  Jane@Example.com ", goodPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "jane@example.com" {
		t.Fatalf("email not normalized: %q", user.Email)
	}
	if user.DisplayName != "jane" {
		t.Fatalf("display name default = %q, want jane", user.DisplayName)
	}
	if _, err := svc.Register(ctx, "jane@example.com", goodPassword, "Jane"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email err = %v, want ErrEmailTaken", err)
	}
}

func TestLoginGenericFailure(t *testing.T) {
	clock := time.Now().UTC()
	svc := newTestService(&clock)
	ctx := context.Background()
	if _, err := svc.Register(ctx, "jane@example.com", goodPassword, "Jane"); err != nil {
		t.Fatal(err)
	}

	user, err := svc.Login(ctx, "JANE@example.com", goodPassword)
	if err != nil || user.Email != "jane@example.com" {
		t.Fatalf("login = %+v %v", user, err)
	}
	if _, err := svc.Login(ctx, "jane@example.com", "wrong password here"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, "nobody@example.com", goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown email err = %v, want ErrInvalidCredentials", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	clock := time.Now().UTC()
	svc := newTestService(&clock)
	ctx := context.Background()
	user, err := svc.Register(ctx, "jane@example.com", goodPassword, "Jane")
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.CreateSession(ctx, user.ID, "test-agent", "203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	if created.Token == "" || created.CSRFToken == "" || created.Session.ID == "" {
		t.Fatalf("created session incomplete: %+v", created)
	}
	if created.Session.ExpiresAt.Sub(created.Session.CreatedAt) != DefaultSessionTTL {
		t.Fatalf("ttl = %v, want %v", created.Session.ExpiresAt.Sub(created.Session.CreatedAt), DefaultSessionTTL)
	}

	gotUser, gotSession, err := svc.ValidateSession(ctx, created.Token)
	if err != nil || gotUser.ID != user.ID || gotSession.ID != created.Session.ID {
		t.Fatalf("validate = %+v %+v %v", gotUser, gotSession, err)
	}
	if _, _, err := svc.ValidateSession(ctx, "not-a-token"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("unknown token err = %v, want ErrSessionInvalid", err)
	}

	// Logout revokes; logout is idempotent.
	if err := svc.Logout(ctx, created.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ValidateSession(ctx, created.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session err = %v, want ErrSessionInvalid", err)
	}
	if err := svc.Logout(ctx, created.Token); err != nil {
		t.Fatalf("idempotent logout: %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	clock := time.Now().UTC()
	svc := newTestService(&clock)
	ctx := context.Background()
	user, _ := svc.Register(ctx, "jane@example.com", goodPassword, "Jane")
	created, err := svc.CreateSession(ctx, user.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(DefaultSessionTTL + time.Minute)
	if _, _, err := svc.ValidateSession(ctx, created.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expired session err = %v, want ErrSessionInvalid", err)
	}
}

func TestSlidingLastSeen(t *testing.T) {
	clock := time.Now().UTC()
	svc := newTestService(&clock)
	ctx := context.Background()
	user, _ := svc.Register(ctx, "jane@example.com", goodPassword, "Jane")
	created, _ := svc.CreateSession(ctx, user.ID, "", "")

	clock = clock.Add(2 * time.Minute)
	if _, _, err := svc.ValidateSession(ctx, created.Token); err != nil {
		t.Fatal(err)
	}
	sessions, err := svc.ListSessions(ctx, user.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("list = %+v %v", sessions, err)
	}
	if !sessions[0].LastSeenAt.Equal(clock) {
		t.Fatalf("last_seen = %v, want %v", sessions[0].LastSeenAt, clock)
	}
}

func TestListAndRevokeSessions(t *testing.T) {
	clock := time.Now().UTC()
	svc := newTestService(&clock)
	ctx := context.Background()
	user, _ := svc.Register(ctx, "jane@example.com", goodPassword, "Jane")
	first, _ := svc.CreateSession(ctx, user.ID, "browser-a", "")
	clock = clock.Add(time.Second)
	second, _ := svc.CreateSession(ctx, user.ID, "browser-b", "")
	clock = clock.Add(time.Second)
	_, _ = svc.CreateSession(ctx, user.ID, "browser-c", "")

	// Another user's session must not be revocable or listed.
	other, _ := svc.Register(ctx, "bob@example.com", goodPassword, "Bob")
	otherSession, _ := svc.CreateSession(ctx, other.ID, "", "")

	sessions, err := svc.ListSessions(ctx, user.ID)
	if err != nil || len(sessions) != 3 {
		t.Fatalf("list = %d %v", len(sessions), err)
	}
	if err := svc.RevokeSession(ctx, user.ID, otherSession.Session.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("cross-user revoke err = %v, want ErrSessionNotFound", err)
	}
	if err := svc.RevokeSession(ctx, user.ID, second.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ValidateSession(ctx, second.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session must be invalid: %v", err)
	}

	n, err := svc.RevokeOthers(ctx, user.ID, first.Session.ID)
	if err != nil || n != 1 {
		t.Fatalf("revoke others = %d %v, want 1", n, err)
	}
	sessions, _ = svc.ListSessions(ctx, user.ID)
	if len(sessions) != 1 || sessions[0].ID != first.Session.ID {
		t.Fatalf("remaining sessions = %+v", sessions)
	}
	if _, _, err := svc.ValidateSession(ctx, first.Token); err != nil {
		t.Fatalf("kept session must remain valid: %v", err)
	}
}

func TestListSessionsFiltersExpired(t *testing.T) {
	clock := time.Now().UTC()
	svc := newTestService(&clock)
	ctx := context.Background()
	user, _ := svc.Register(ctx, "jane@example.com", goodPassword, "Jane")
	_, _ = svc.CreateSession(ctx, user.ID, "", "")
	clock = clock.Add(DefaultSessionTTL + time.Hour)
	sessions, err := svc.ListSessions(ctx, user.ID)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("expired sessions listed: %+v %v", sessions, err)
	}
}

func TestCreateSessionRequiresUser(t *testing.T) {
	clock := time.Now().UTC()
	svc := newTestService(&clock)
	if _, err := svc.CreateSession(context.Background(), "", "", ""); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("empty user err = %v, want ErrUserNotFound", err)
	}
}
