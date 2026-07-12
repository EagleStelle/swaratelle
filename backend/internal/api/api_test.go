package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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

func assertNoStatusField(t *testing.T, record map[string]any) {
	t.Helper()

	if _, ok := record["Status"]; ok {
		t.Fatalf("record includes Status field: %#v", record)
	}
	if _, ok := record["status"]; ok {
		t.Fatalf("record includes status field: %#v", record)
	}
}
