package auth

import (
	"context"
	"path/filepath"
	"testing"

	"iwaradl-managed/internal/db"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return NewManager(database)
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash := hashPassword("correct horse")
	if !passwordHashLooksValid(hash) {
		t.Fatalf("hash does not look valid: %q", hash)
	}
	if !verifyPassword("correct horse", hash) {
		t.Fatal("verifyPassword rejected the correct password")
	}
	if verifyPassword("wrong password", hash) {
		t.Fatal("verifyPassword accepted a wrong password")
	}
}

func TestEnsureAuthSettingsSeedsDefaultsAndIsIdempotent(t *testing.T) {
	// Env not set in this test => defaults root/swaratelle.
	t.Setenv(envUsername, "")
	t.Setenv(envPassword, "")
	m := newTestManager(t)
	ctx := context.Background()

	first, err := m.EnsureAuthSettings(ctx)
	if err != nil {
		t.Fatalf("EnsureAuthSettings: %v", err)
	}
	if first.Username != defaultUsername {
		t.Fatalf("username = %q, want %q", first.Username, defaultUsername)
	}
	if !verifyPassword(defaultPassword, first.PasswordHash) {
		t.Fatal("seeded password hash does not verify against the default password")
	}
	if len(first.SessionSecret) < 32 {
		t.Fatalf("session secret too short: %d", len(first.SessionSecret))
	}
	if first.SessionVersion != 1 {
		t.Fatalf("session version = %d, want 1", first.SessionVersion)
	}

	second, err := m.EnsureAuthSettings(ctx)
	if err != nil {
		t.Fatalf("EnsureAuthSettings second call: %v", err)
	}
	if second != first {
		t.Fatalf("second ensure changed settings: %#v vs %#v", second, first)
	}
}

func TestEnsureAuthSettingsSeedsFromEnv(t *testing.T) {
	t.Setenv(envUsername, "operator")
	t.Setenv(envPassword, "hunter2hunter2")
	m := newTestManager(t)

	s, err := m.EnsureAuthSettings(context.Background())
	if err != nil {
		t.Fatalf("EnsureAuthSettings: %v", err)
	}
	if s.Username != "operator" {
		t.Fatalf("username = %q, want operator", s.Username)
	}
	if !verifyPassword("hunter2hunter2", s.PasswordHash) {
		t.Fatal("env password did not seed")
	}
}

func TestAuthenticate(t *testing.T) {
	t.Setenv(envUsername, "root")
	t.Setenv(envPassword, "swaratelle")
	m := newTestManager(t)
	ctx := context.Background()

	if _, err := m.Authenticate(ctx, "root", "swaratelle"); err != nil {
		t.Fatalf("Authenticate valid: %v", err)
	}
	if _, err := m.Authenticate(ctx, "root", "wrong"); err != ErrInvalidCredentials {
		t.Fatalf("wrong password err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := m.Authenticate(ctx, "nobody", "swaratelle"); err != ErrInvalidCredentials {
		t.Fatalf("wrong username err = %v, want ErrInvalidCredentials", err)
	}
}

func TestSessionTokenRoundTripAndTamper(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	settings, err := m.EnsureAuthSettings(ctx)
	if err != nil {
		t.Fatalf("EnsureAuthSettings: %v", err)
	}

	token, err := m.CreateSessionToken(settings)
	if err != nil {
		t.Fatalf("CreateSessionToken: %v", err)
	}
	sess, err := m.ReadSessionToken(ctx, token)
	if err != nil {
		t.Fatalf("ReadSessionToken: %v", err)
	}
	if sess == nil || sess.Username != settings.Username {
		t.Fatalf("session = %#v, want username %q", sess, settings.Username)
	}

	if sess, _ := m.ReadSessionToken(ctx, token+"x"); sess != nil {
		t.Fatal("tampered token was accepted")
	}
	if sess, _ := m.ReadSessionToken(ctx, "garbage"); sess != nil {
		t.Fatal("garbage token was accepted")
	}
}

func TestUpdateCredentialsBumpsVersionAndInvalidatesOldToken(t *testing.T) {
	t.Setenv(envUsername, "root")
	t.Setenv(envPassword, "swaratelle")
	m := newTestManager(t)
	ctx := context.Background()

	settings, err := m.EnsureAuthSettings(ctx)
	if err != nil {
		t.Fatalf("EnsureAuthSettings: %v", err)
	}
	oldToken, err := m.CreateSessionToken(settings)
	if err != nil {
		t.Fatalf("CreateSessionToken: %v", err)
	}

	updated, err := m.UpdateCredentials(ctx, "swaratelle", "newname", "newpassword123")
	if err != nil {
		t.Fatalf("UpdateCredentials: %v", err)
	}
	if updated.Username != "newname" {
		t.Fatalf("username = %q, want newname", updated.Username)
	}
	if updated.SessionVersion != settings.SessionVersion+1 {
		t.Fatalf("version = %d, want %d", updated.SessionVersion, settings.SessionVersion+1)
	}
	if !verifyPassword("newpassword123", updated.PasswordHash) {
		t.Fatal("new password did not take")
	}

	// Old token must no longer validate (version changed).
	if sess, _ := m.ReadSessionToken(ctx, oldToken); sess != nil {
		t.Fatal("old token still valid after credential change")
	}
	// New login works with the new credentials.
	if _, err := m.Authenticate(ctx, "newname", "newpassword123"); err != nil {
		t.Fatalf("Authenticate with new creds: %v", err)
	}
}

func TestUpdateCredentialsValidation(t *testing.T) {
	t.Setenv(envUsername, "root")
	t.Setenv(envPassword, "swaratelle")
	m := newTestManager(t)
	ctx := context.Background()
	if _, err := m.EnsureAuthSettings(ctx); err != nil {
		t.Fatalf("EnsureAuthSettings: %v", err)
	}

	var v *ValidationError
	if _, err := m.UpdateCredentials(ctx, "wrong-current", "x", ""); err == nil {
		t.Fatal("expected error for wrong current password")
	} else if !asValidation(err, &v) {
		t.Fatalf("err = %v, want ValidationError", err)
	}

	if _, err := m.UpdateCredentials(ctx, "swaratelle", "root", "short"); err == nil {
		t.Fatal("expected error for short password")
	} else if !asValidation(err, &v) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
}

func asValidation(err error, target **ValidationError) bool {
	ve, ok := err.(*ValidationError)
	if ok {
		*target = ve
	}
	return ok
}
