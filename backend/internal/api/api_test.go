package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iwaradl-managed/internal/auth"
	"iwaradl-managed/internal/db"
	"iwaradl-managed/internal/downloader"
)

func TestActiveDownloadsResponseOmitsStatus(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.DB.MarkPending(ctx, "active-1", "https://example.com/active-1"); err != nil {
		t.Fatalf("MarkPending returned error: %v", err)
	}
	if err := server.DB.MarkFailed(ctx, "active-1", "network unavailable"); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleActiveDownloads(rr, httptest.NewRequest(http.MethodGet, "/api/downloads/active", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	var records []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&records); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	assertNoStatusField(t, records[0])
	if got, want := records[0]["Error"], "network unavailable"; got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}

func TestHistoryResponseOmitsStatus(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if _, err := server.DB.InsertReconciled(
		ctx,
		"history-1",
		"https://example.com/history-1",
		filepath.Join(t.TempDir(), "history-1.mp4"),
		"History One",
		"Artist",
		1024,
	); err != nil {
		t.Fatalf("InsertReconciled returned error: %v", err)
	}
	if err := server.DB.MarkPending(ctx, "failed-1", "https://example.com/failed-1"); err != nil {
		t.Fatalf("MarkPending returned error: %v", err)
	}
	if err := server.DB.MarkFailed(ctx, "failed-1", "network unavailable"); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleHistory(rr, httptest.NewRequest(http.MethodGet, "/api/history?limit=50", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	var response struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(response.Records))
	}
	assertNoStatusField(t, response.Records[0])
}

func TestDeleteDownloadRecordRemovesRow(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.DB.MarkPending(ctx, "failed-1", "https://example.com/failed-1"); err != nil {
		t.Fatalf("MarkPending returned error: %v", err)
	}
	if err := server.DB.MarkFailed(ctx, "failed-1", "network unavailable"); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleDownloadRecord(rr, httptest.NewRequest(http.MethodDelete, "/api/downloads/failed-1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	rec, err := server.DB.Get(ctx, "failed-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected deleted record, got %#v", rec)
	}
}

func TestDeleteDownloadRecordCancelsPendingRow(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.DB.MarkPending(ctx, "pending-1", "https://example.com/pending-1"); err != nil {
		t.Fatalf("MarkPending returned error: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleDownloadRecord(rr, httptest.NewRequest(http.MethodDelete, "/api/downloads/pending-1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	rec, err := server.DB.Get(ctx, "pending-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected pending record to be deleted, got %#v", rec)
	}
}

func TestDeleteDownloadRecordRejectsDoneRow(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if _, err := server.DB.InsertReconciled(
		ctx,
		"done-1",
		"https://example.com/done-1",
		filepath.Join(t.TempDir(), "done-1.mp4"),
		"Done One",
		"Artist",
		1024,
	); err != nil {
		t.Fatalf("InsertReconciled returned error: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleDownloadRecord(rr, httptest.NewRequest(http.MethodDelete, "/api/downloads/done-1", nil))

	if rr.Code != http.StatusConflict {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusConflict)
	}
	rec, err := server.DB.Get(ctx, "done-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected done record to remain")
	}
}

func TestDownloadFileServesCompletedMedia(t *testing.T) {
	server := newTestServerWithAuth(t)
	ctx := context.Background()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "Clip One [vid-1].mp4")
	payload := []byte("fake-mp4-bytes")
	if err := os.WriteFile(filePath, payload, 0o600); err != nil {
		t.Fatalf("write media file: %v", err)
	}
	if _, err := server.DB.InsertReconciled(ctx, "vid-1", "https://example.com/vid-1", filePath, "Clip One", "Artist", int64(len(payload))); err != nil {
		t.Fatalf("InsertReconciled returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/downloads/vid-1/file", nil)
	req.Header.Set("Authorization", "Bearer "+server.Token)
	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Body.String(); got != string(payload) {
		t.Fatalf("body = %q, want %q", got, string(payload))
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "Clip One [vid-1].mp4") {
		t.Fatalf("Content-Disposition = %q, want attachment with filename", cd)
	}
}

func TestDownloadFileRejectsIncomplete(t *testing.T) {
	server := newTestServerWithAuth(t)
	ctx := context.Background()
	if err := server.DB.MarkPending(ctx, "pending-1", "https://example.com/pending-1"); err != nil {
		t.Fatalf("MarkPending returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/downloads/pending-1/file", nil)
	req.Header.Set("Authorization", "Bearer "+server.Token)
	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusConflict)
	}
}

func TestDownloadFileMissingRowIsNotFound(t *testing.T) {
	server := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/api/downloads/nope/file", nil)
	req.Header.Set("Authorization", "Bearer "+server.Token)
	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestDownloadFileMissingOnDiskIsNotFound(t *testing.T) {
	server := newTestServerWithAuth(t)
	ctx := context.Background()
	// A done row whose file was deleted out from under it.
	gonePath := filepath.Join(t.TempDir(), "gone [vid-2].mp4")
	if _, err := server.DB.InsertReconciled(ctx, "vid-2", "https://example.com/vid-2", gonePath, "Gone", "Artist", 10); err != nil {
		t.Fatalf("InsertReconciled returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/downloads/vid-2/file", nil)
	req.Header.Set("Authorization", "Bearer "+server.Token)
	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestDownloadFileRequiresAuth(t *testing.T) {
	server := newTestServerWithAuth(t)

	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/downloads/vid-1/file", nil))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestHandleCountsGroupsByStatus(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()

	// One done row via reconcile, one pending, one failed.
	if _, err := server.DB.InsertReconciled(
		ctx,
		"done-1",
		"https://example.com/done-1",
		filepath.Join(t.TempDir(), "done-1.mp4"),
		"Done One",
		"Artist",
		1024,
	); err != nil {
		t.Fatalf("InsertReconciled returned error: %v", err)
	}
	if err := server.DB.MarkPending(ctx, "pending-1", "https://example.com/pending-1"); err != nil {
		t.Fatalf("MarkPending returned error: %v", err)
	}
	if err := server.DB.MarkPending(ctx, "failed-1", "https://example.com/failed-1"); err != nil {
		t.Fatalf("MarkPending returned error: %v", err)
	}
	if err := server.DB.MarkFailed(ctx, "failed-1", "network unavailable"); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleCounts(rr, httptest.NewRequest(http.MethodGet, "/api/counts", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	var got countsResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := countsResponse{Pending: 1, Downloading: 0, Done: 1, Failed: 1, Total: 3}
	if got != want {
		t.Fatalf("counts = %#v, want %#v", got, want)
	}
}

func TestHandleQueueResolvesOreno3DURLBeforeQueueing(t *testing.T) {
	server := newTestServer(t)
	const (
		orenoURL = "https://oreno3d.com/movies/347601"
		iwaraURL = "https://www.iwara.tv/video/CNCQNZEfKO8QYI/mmdparty-tonight-jane-doe"
	)
	server.ResolveURL = func(ctx context.Context, raw string) (string, error) {
		if raw != orenoURL {
			t.Fatalf("ResolveURL raw = %q, want %q", raw, orenoURL)
		}
		return iwaraURL, nil
	}

	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"urls":["` + orenoURL + `"]}`)
	server.handleQueue(rr, httptest.NewRequest(http.MethodPost, "/api/queue", body))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	var results []queueResult
	if err := json.NewDecoder(rr.Body).Decode(&results); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Status != "queued" {
		t.Fatalf("Status = %q, want queued", results[0].Status)
	}
	if results[0].VideoID != "CNCQNZEfKO8QYI" {
		t.Fatalf("VideoID = %q, want CNCQNZEfKO8QYI", results[0].VideoID)
	}

	rec, err := server.DB.Get(context.Background(), "CNCQNZEfKO8QYI")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected queued record")
	}
	if rec.SourceURL != iwaraURL {
		t.Fatalf("SourceURL = %q, want %q", rec.SourceURL, iwaraURL)
	}
}

func TestBusinessEndpointRequiresAuth(t *testing.T) {
	server := newTestServerWithAuth(t)

	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/history", nil))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// TestBusinessEndpointsAllRequireAuth is the lockdown guard: every /api/* route
// that touches data must return 401 before running any handler logic when the
// request carries no bearer token and no session cookie. It drives the real mux
// (not the handlers directly) so routing, method matching, and the auth wrapper
// are all exercised together. Add every new business route here.
func TestBusinessEndpointsAllRequireAuth(t *testing.T) {
	server := newTestServerWithAuth(t)

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/downloads"},
		{http.MethodGet, "/api/downloads/active"},
		{http.MethodGet, "/api/downloads/some-id/file"},
		{http.MethodDelete, "/api/downloads/some-id"},
		{http.MethodGet, "/api/history"},
		{http.MethodGet, "/api/counts"},
		{http.MethodPost, "/api/queue"},
		{http.MethodPost, "/api/scan"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.Routes().ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status code = %d, want %d (body: %s)", rr.Code, http.StatusUnauthorized, rr.Body.String())
			}
		})
	}
}

// TestAuthSessionDoesNotLeakToUnauthenticated confirms the one unauthenticated
// data endpoint (the SPA's session probe) reveals nothing about the account to a
// caller with no valid cookie: authenticated=false and an empty username.
func TestAuthSessionDoesNotLeakToUnauthenticated(t *testing.T) {
	server := newTestServerWithAuth(t)

	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp authSessionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Authenticated {
		t.Fatal("unauthenticated session probe reported authenticated=true")
	}
	if resp.Username != "" {
		t.Fatalf("username leaked to unauthenticated caller: %q", resp.Username)
	}
}

// TestDownloadFileWrongBearerRejected confirms a bad token cannot pull bytes even
// for a real completed row -- the file endpoint fails closed like the rest.
func TestDownloadFileWrongBearerRejected(t *testing.T) {
	server := newTestServerWithAuth(t)
	ctx := context.Background()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "Clip [vid-x].mp4")
	if err := os.WriteFile(filePath, []byte("secret-bytes"), 0o600); err != nil {
		t.Fatalf("write media file: %v", err)
	}
	if _, err := server.DB.InsertReconciled(ctx, "vid-x", "https://example.com/vid-x", filePath, "Clip", "Artist", 12); err != nil {
		t.Fatalf("InsertReconciled returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/downloads/vid-x/file", nil)
	req.Header.Set("Authorization", "Bearer not-the-real-token")
	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if strings.Contains(rr.Body.String(), "secret-bytes") {
		t.Fatal("rejected request still leaked file bytes")
	}
}

func TestBearerTokenAuthorizes(t *testing.T) {
	server := newTestServerWithAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	req.Header.Set("Authorization", "Bearer "+server.Token)
	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestLoginSetsCookieThatAuthorizes(t *testing.T) {
	server := newTestServerWithAuth(t)

	cookie := loginForCookie(t, server, "root", "swaratelle")

	req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("authorized request status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	server := newTestServerWithAuth(t)

	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"root","password":"nope"}`)
	server.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/auth/login", body))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestCredentialsUpdateFlow(t *testing.T) {
	server := newTestServerWithAuth(t)
	cookie := loginForCookie(t, server, "root", "swaratelle")

	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"current_password":"swaratelle","username":"root","new_password":"newpassword123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/credentials", body)
	req.AddCookie(cookie)
	server.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("credentials update status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}

	// New password now works; old one no longer does.
	loginForCookie(t, server, "root", "newpassword123")
	if code := loginStatus(server, "root", "swaratelle"); code != http.StatusUnauthorized {
		t.Fatalf("old password login status = %d, want %d", code, http.StatusUnauthorized)
	}
}

func loginStatus(server *Server, username, password string) int {
	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"` + username + `","password":"` + password + `"}`)
	server.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/auth/login", body))
	return rr.Code
}

func loginForCookie(t *testing.T, server *Server, username, password string) *http.Cookie {
	t.Helper()
	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"` + username + `","password":"` + password + `"}`)
	server.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/auth/login", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.SessionCookie {
			return c
		}
	}
	t.Fatal("login did not set a session cookie")
	return nil
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "downloads.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	return &Server{
		DB: database,
		DL: &downloader.Downloader{},
	}
}

func newTestServerWithAuth(t *testing.T) *Server {
	t.Helper()
	t.Setenv("SWARATELLE_USERNAME", "root")
	t.Setenv("SWARATELLE_PASSWORD", "swaratelle")

	database, err := db.Open(filepath.Join(t.TempDir(), "downloads.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	authMgr := auth.NewManager(database)
	if _, err := authMgr.EnsureAuthSettings(context.Background()); err != nil {
		t.Fatalf("EnsureAuthSettings returned error: %v", err)
	}

	return &Server{
		DB:    database,
		DL:    &downloader.Downloader{},
		Auth:  authMgr,
		Token: "test-bearer-token",
	}
}

func assertNoStatusField(t *testing.T, record map[string]any) {
	t.Helper()

	if _, ok := record["Status"]; ok {
		t.Fatalf("record includes Status field: %#v", record)
	}
	if _, ok := record["status"]; ok {
		t.Fatalf("record includes status field: %#v", record)
	}
}
