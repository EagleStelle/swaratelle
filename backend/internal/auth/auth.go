// Package auth provides the web-UI login: a seeded username/password account,
// a signed session cookie, and credential updates. It is only a lock on the
// bundled UI so not everyone on the LAN can drive the downloader; external API
// clients keep using the SWARATELLE_API_TOKEN bearer instead and never log in.
//
// The stored format mirrors never-stelle (EagleStelle/never-stelle) so the two
// appliances share one mental model: a pbkdf2_sha256 hash string and an
// HMAC-signed "base64(json).signature" session token, seeded from env on first
// boot.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"iwaradl-managed/internal/db"
)

const (
	// SessionCookie is the cookie the bundled UI authenticates with.
	SessionCookie = "swaratelle_session"

	authSettingsKey = "auth"

	defaultUsername   = "root"
	defaultPassword   = "swaratelle"
	pbkdf2Iterations  = 390_000
	sessionMaxAge     = 30 * 24 * time.Hour
	minPasswordLength = 8
	maxUsernameLength = 120

	envUsername     = "SWARATELLE_USERNAME"
	envPassword     = "SWARATELLE_PASSWORD"
	envCookieSecure = "SWARATELLE_COOKIE_SECURE"
)

// ErrInvalidCredentials is a failed login (wrong username or password). The API
// maps it to 401; every other auth error is a 400 validation problem.
var ErrInvalidCredentials = errors.New("invalid username or password")

// ValidationError carries a user-facing message for a rejected credential
// change (bad current password, username/password rules). The API returns it as
// a 400 with the message verbatim.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func validationErr(msg string) error { return &ValidationError{Message: msg} }

// Settings is the persisted auth payload (stored as JSON under the "auth"
// settings key). Field names match never-stelle's payload.
type Settings struct {
	Username       string `json:"username"`
	PasswordHash   string `json:"password_hash"`
	SessionSecret  string `json:"session_secret"`
	SessionVersion int    `json:"session_version"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

// Session is the authenticated identity read back from a valid cookie token.
type Session struct {
	Username  string
	ExpiresAt int64
}

// Manager owns the auth settings lifecycle against the DB. Its read-modify-write
// operations are serialized so concurrent logins/seeds don't clobber each other.
type Manager struct {
	db           *db.DB
	cookieSecure bool
	mu           sync.Mutex
}

func NewManager(database *db.DB) *Manager {
	return &Manager{
		db:           database,
		cookieSecure: cookieSecureEnv(),
	}
}

func cookieSecureEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envCookieSecure))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// --- base64 (pad-stripped url-safe, matching never-stelle) ---

func b64encode(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func b64decode(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(value)
}

// --- PBKDF2-HMAC-SHA256 (inline; no external dependency) ---

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	dk := make([]byte, 0, numBlocks*hashLen)
	blockIdx := make([]byte, 4)
	u := make([]byte, hashLen)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		blockIdx[0] = byte(block >> 24)
		blockIdx[1] = byte(block >> 16)
		blockIdx[2] = byte(block >> 8)
		blockIdx[3] = byte(block)
		prf.Write(blockIdx)
		t := prf.Sum(nil)
		copy(u, t)
		for n := 2; n <= iterations; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for i := range t {
				t[i] ^= u[i]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

func hashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	digest := pbkdf2SHA256([]byte(password), salt, pbkdf2Iterations, sha256.Size)
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s", pbkdf2Iterations, b64encode(salt), b64encode(digest))
}

func verifyPassword(password, encoded string) bool {
	algorithm, iterations, salt, digest, ok := parsePasswordHash(encoded)
	if !ok || algorithm != "pbkdf2_sha256" || iterations <= 0 {
		return false
	}
	candidate := pbkdf2SHA256([]byte(password), salt, iterations, len(digest))
	return hmac.Equal(candidate, digest)
}

func passwordHashLooksValid(encoded string) bool {
	algorithm, iterations, _, _, ok := parsePasswordHash(encoded)
	return ok && algorithm == "pbkdf2_sha256" && iterations > 0
}

func parsePasswordHash(encoded string) (algorithm string, iterations int, salt, digest []byte, ok bool) {
	parts := strings.SplitN(encoded, "$", 4)
	if len(parts) != 4 {
		return "", 0, nil, nil, false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil {
		return "", 0, nil, nil, false
	}
	salt, err := b64decode(parts[2])
	if err != nil {
		return "", 0, nil, nil, false
	}
	digest, err = b64decode(parts[3])
	if err != nil {
		return "", 0, nil, nil, false
	}
	return parts[0], iterations, salt, digest, true
}

// --- settings load / seed / save ---

func (m *Manager) load(ctx context.Context) (Settings, error) {
	raw, ok, err := m.db.GetSetting(ctx, authSettingsKey)
	if err != nil || !ok {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Settings{}, nil // treat corrupt payload as empty; EnsureAuthSettings reseeds
	}
	return s, nil
}

func (m *Manager) save(ctx context.Context, s Settings) error {
	payload, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return m.db.SetSetting(ctx, authSettingsKey, string(payload))
}

func seedUsername() string {
	if v := strings.TrimSpace(os.Getenv(envUsername)); v != "" {
		return v
	}
	return defaultUsername
}

func seedPassword() string {
	if v := strings.TrimSpace(os.Getenv(envPassword)); v != "" {
		return v
	}
	return defaultPassword
}

// EnsureAuthSettings loads the stored auth payload and fills in any missing
// pieces — this is the seed: on first boot it plants the env (or default)
// username/password and a fresh random session secret. It only writes when
// something actually changed, so it is cheap to call on every request.
func (m *Manager) EnsureAuthSettings(ctx context.Context) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureLocked(ctx)
}

func (m *Manager) ensureLocked(ctx context.Context) (Settings, error) {
	current, err := m.load(ctx)
	if err != nil {
		return Settings{}, err
	}
	now := time.Now().Unix()

	next := current
	if strings.TrimSpace(next.Username) == "" {
		next.Username = seedUsername()
	} else {
		next.Username = strings.TrimSpace(next.Username)
	}
	if next.PasswordHash == "" || !passwordHashLooksValid(next.PasswordHash) {
		next.PasswordHash = hashPassword(seedPassword())
	}
	if len(next.SessionSecret) < 32 {
		secret := make([]byte, 32)
		_, _ = rand.Read(secret)
		next.SessionSecret = b64encode(secret)
	}
	if next.SessionVersion < 1 {
		next.SessionVersion = 1
	}
	if next.CreatedAt == 0 {
		next.CreatedAt = now
	}
	if next.UpdatedAt == 0 {
		next.UpdatedAt = now
	}

	if next != current {
		if err := m.save(ctx, next); err != nil {
			return Settings{}, err
		}
	}
	return next, nil
}

// Authenticate verifies a login. Both checks run regardless of outcome to keep
// timing uniform, and the comparisons are constant-time.
func (m *Manager) Authenticate(ctx context.Context, username, password string) (Settings, error) {
	auth, err := m.EnsureAuthSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	usernameOK := subtle.ConstantTimeCompare(
		[]byte(strings.TrimSpace(username)), []byte(auth.Username)) == 1
	passwordOK := verifyPassword(password, auth.PasswordHash)
	if !usernameOK || !passwordOK {
		return Settings{}, ErrInvalidCredentials
	}
	return auth, nil
}

// --- session token ---

type sessionBody struct {
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
	Ver int    `json:"ver"`
}

func sign(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return b64encode(mac.Sum(nil))
}

// CreateSessionToken issues a signed cookie value bound to the current username
// and session version, so a later credential change invalidates it.
func (m *Manager) CreateSessionToken(auth Settings) (string, error) {
	now := time.Now()
	payload, err := json.Marshal(sessionBody{
		Sub: auth.Username,
		Iat: now.Unix(),
		Exp: now.Add(sessionMaxAge).Unix(),
		Ver: auth.SessionVersion,
	})
	if err != nil {
		return "", err
	}
	body := b64encode(payload)
	return body + "." + sign(body, auth.SessionSecret), nil
}

// ReadSessionToken validates a cookie value and returns the identity, or nil if
// the token is missing, tampered, expired, or superseded by a version bump.
func (m *Manager) ReadSessionToken(ctx context.Context, token string) (*Session, error) {
	body, signature, found := strings.Cut(token, ".")
	if !found {
		return nil, nil
	}
	auth, err := m.EnsureAuthSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !hmac.Equal([]byte(signature), []byte(sign(body, auth.SessionSecret))) {
		return nil, nil
	}
	raw, err := b64decode(body)
	if err != nil {
		return nil, nil
	}
	var payload sessionBody
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil
	}
	if payload.Exp < time.Now().Unix() {
		return nil, nil
	}
	if payload.Sub != auth.Username || payload.Ver != auth.SessionVersion {
		return nil, nil
	}
	return &Session{Username: auth.Username, ExpiresAt: payload.Exp}, nil
}

// SessionFromRequest reads and validates the session cookie on a request.
func (m *Manager) SessionFromRequest(ctx context.Context, r *http.Request) (*Session, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return nil, nil
	}
	return m.ReadSessionToken(ctx, c.Value)
}

// UpdateCredentials changes the username and/or password after verifying the
// current password. Any change bumps the session version, which invalidates all
// existing cookies (the caller re-issues one for the current session).
func (m *Manager) UpdateCredentials(ctx context.Context, currentPassword, username, newPassword string) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	auth, err := m.ensureLocked(ctx)
	if err != nil {
		return Settings{}, err
	}
	if !verifyPassword(currentPassword, auth.PasswordHash) {
		return Settings{}, validationErr("Current password is incorrect.")
	}

	nextUsername := strings.TrimSpace(username)
	if nextUsername == "" {
		nextUsername = auth.Username
	}
	if nextUsername == "" {
		return Settings{}, validationErr("Username is required.")
	}
	if len(nextUsername) > maxUsernameLength {
		return Settings{}, validationErr("Username is too long.")
	}
	if newPassword != "" && len(newPassword) < minPasswordLength {
		return Settings{}, validationErr(fmt.Sprintf("Password must be at least %d characters.", minPasswordLength))
	}

	updated := auth
	changed := false
	if nextUsername != auth.Username {
		updated.Username = nextUsername
		changed = true
	}
	if newPassword != "" {
		updated.PasswordHash = hashPassword(newPassword)
		changed = true
	}
	if changed {
		updated.SessionVersion = auth.SessionVersion + 1
		updated.UpdatedAt = time.Now().Unix()
		if err := m.save(ctx, updated); err != nil {
			return Settings{}, err
		}
	}
	return updated, nil
}

// --- cookies ---

func (m *Manager) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   m.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}
