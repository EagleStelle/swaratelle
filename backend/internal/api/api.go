package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"iwaradl-managed/internal/db"
	"iwaradl-managed/internal/downloader"
)

type Server struct {
	DL         *downloader.Downloader
	DB         *db.DB
	Token      string
	WebDir     string
	ResolveURL func(context.Context, string) (string, error)
}

type queueRequest struct {
	URLs []string `json:"urls"`
}

type queueResult struct {
	URL     string `json:"url"`
	VideoID string `json:"video_id,omitempty"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

type recordResponse struct {
	VideoID   string
	SourceURL string
	FilePath  string
	FileSize  int64
	Title     string
	Artist    string
	Error     string
	Attempts  int
	CreatedAt int64
	UpdatedAt int64
	Progress  int
}

type historyResponse struct {
	Records    []recordResponse `json:"records"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// countsResponse exposes the per-status row totals so clients can read the
// completed count directly instead of walking every history page.
type countsResponse struct {
	Pending     int `json:"pending"`
	Downloading int `json:"downloading"`
	Done        int `json:"done"`
	Failed      int `json:"failed"`
	Total       int `json:"total"`
}

type historyCursor struct {
	UpdatedAt int64  `json:"updated_at"`
	VideoID   string `json:"video_id"`
}

const (
	defaultHistoryLimit = 50
	maxHistoryLimit     = 100
)

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/downloads/active", s.auth(s.handleActiveDownloads))
	mux.HandleFunc("/api/downloads/", s.auth(s.handleDownloadRecord))
	mux.HandleFunc("/api/downloads", s.auth(s.handleDownloads))
	mux.HandleFunc("/api/history", s.auth(s.handleHistory))
	mux.HandleFunc("/api/counts", s.auth(s.handleCounts))
	mux.HandleFunc("/api/queue", s.auth(s.handleQueue))
	mux.HandleFunc("/api/scan", s.auth(s.handleScan))
	if s.WebDir != "" {
		mux.Handle("/", s.withSession(s.staticHandler()))
	}
	return mux
}

// staticHandler serves the Next.js static export with clean-URL resolution and
// no directory listing. Next emits a route page as "<route>.html" plus a
// "<route>/" folder of RSC segment payloads; the stdlib http.FileServer would
// happily list that folder (leaking the "__next.*.txt" internals) and never map
// "/history" to "history.html". This resolves, in order: the exact path, then
// "<path>.html", then "<path>/index.html", and otherwise serves 404.html. It
// never serves a directory, so nothing gets listed.
func (s *Server) staticHandler() http.Handler {
	root := http.Dir(s.WebDir)

	serveFile := func(w http.ResponseWriter, r *http.Request, name string) bool {
		f, err := root.Open(name)
		if err != nil {
			return false
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			return false
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
		return true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := path.Clean("/" + r.URL.Path)

		candidates := []string{upath}
		if path.Ext(upath) == "" {
			// Extensionless => a route, not an asset. Prefer the rendered page.
			trimmed := strings.TrimSuffix(upath, "/")
			candidates = []string{trimmed + ".html", trimmed + "/index.html", upath + "/index.html"}
			if upath == "/" {
				candidates = []string{"/index.html"}
			}
		}
		for _, c := range candidates {
			if serveFile(w, r, c) {
				return
			}
		}

		if f, err := root.Open("/404.html"); err == nil {
			defer f.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.Copy(w, f)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 not found"))
	})
}

const sessionCookie = "swaratelle_session"

// withSession plants the API token as a same-site cookie while serving the
// bundled UI, so the browser authenticates automatically against the
// same-origin API. The user never has to paste the token; external API clients
// still use the Bearer header.
func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookie,
				Value:    s.Token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" && !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// authorized accepts either a Bearer header (external clients) or the session
// cookie set by withSession (the bundled UI).
func (s *Server) authorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") && strings.TrimPrefix(h, "Bearer ") == s.Token {
		return true
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value == s.Token {
		return true
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	records, err := s.DB.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.overlayProgress(records)
	writeJSON(w, http.StatusOK, recordResponses(records))
}

func (s *Server) handleDownloadRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	videoID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/downloads/"), "/")
	if videoID == "" || strings.Contains(videoID, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid video id"})
		return
	}
	rec, err := s.DB.Get(r.Context(), videoID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rec != nil && rec.Status == db.StatusDone {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "completed downloads cannot be removed here"})
		return
	}
	if rec != nil && (rec.Status == db.StatusPending || rec.Status == db.StatusDownloading) {
		cancelCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := s.DL.Cancel(cancelCtx, videoID); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := s.DB.Delete(r.Context(), videoID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleActiveDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	records, err := s.DB.ListActive(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.overlayProgress(records)
	writeJSON(w, http.StatusOK, recordResponses(records))
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	limit, err := parseHistoryLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cursor, err := decodeHistoryCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))

	records, err := s.DB.ListHistory(r.Context(), limit+1, cursor, search)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	nextCursor := ""
	if len(records) > limit {
		records = records[:limit]
		nextCursor, err = encodeHistoryCursor(records[len(records)-1])
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, historyResponse{
		Records:    recordResponses(records),
		NextCursor: nextCursor,
	})
}

func (s *Server) handleCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	counts, err := s.DB.Counts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, countsResponse{
		Pending:     counts.Pending,
		Downloading: counts.Downloading,
		Done:        counts.Done,
		Failed:      counts.Failed,
		Total:       counts.Total,
	})
}

func recordResponses(records []db.Record) []recordResponse {
	out := make([]recordResponse, 0, len(records))
	for _, r := range records {
		out = append(out, recordResponse{
			VideoID:   r.VideoID,
			SourceURL: r.SourceURL,
			FilePath:  r.FilePath,
			FileSize:  r.FileSize,
			Title:     r.Title,
			Artist:    r.Artist,
			Error:     r.Error,
			Attempts:  r.Attempts,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			Progress:  r.Progress,
		})
	}
	return out
}

func (s *Server) overlayProgress(records []db.Record) {
	// Overlay live progress onto in-flight rows; it lives only in memory.
	for i := range records {
		if records[i].Status == db.StatusDownloading {
			if p, ok := s.DL.Progress(records[i].VideoID); ok {
				records[i].Progress = p
			}
		}
	}
}

func parseHistoryLimit(raw string) (int, error) {
	if raw == "" {
		return defaultHistoryLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		return 0, errors.New("limit must be a positive integer")
	}
	if limit > maxHistoryLimit {
		return maxHistoryLimit, nil
	}
	return limit, nil
}

func encodeHistoryCursor(record db.Record) (string, error) {
	payload, err := json.Marshal(historyCursor{
		UpdatedAt: record.UpdatedAt,
		VideoID:   record.VideoID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeHistoryCursor(raw string) (*db.PageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("cursor is invalid")
	}
	var cursor historyCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return nil, errors.New("cursor is invalid")
	}
	if cursor.VideoID == "" {
		return nil, errors.New("cursor is invalid")
	}
	return &db.PageCursor{
		UpdatedAt: cursor.UpdatedAt,
		VideoID:   cursor.VideoID,
	}, nil
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req queueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if len(req.URLs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no urls provided"})
		return
	}

	results := make([]queueResult, 0, len(req.URLs))
	for _, url := range req.URLs {
		url = strings.TrimSpace(url)
		res := queueResult{URL: url}
		resolvedURL, err := s.resolveURL(r.Context(), url)
		if err != nil {
			res.Status = "rejected"
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		vid, err := downloader.ExtractVideoID(resolvedURL)
		if err != nil {
			res.Status = "rejected"
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		res.VideoID = vid

		// Skip work that's already done or in flight; only (re)queue new or
		// previously failed videos.
		if rec, err := s.DB.Get(r.Context(), vid); err == nil && rec != nil &&
			(rec.Status == db.StatusDone || rec.Status == db.StatusDownloading || rec.Status == db.StatusPending) {
			res.Status = string(rec.Status)
			results = append(results, res)
			continue
		}

		// Record it as pending so it shows up instantly, then hand off to the
		// on-demand scheduler and return without waiting for the download.
		if err := s.DB.MarkPending(r.Context(), vid, resolvedURL); err != nil {
			res.Status = "failed"
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		s.DL.Enqueue(resolvedURL)
		res.Status = "queued"
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) resolveURL(ctx context.Context, raw string) (string, error) {
	if s.ResolveURL != nil {
		return s.ResolveURL(ctx, raw)
	}
	return downloader.ResolveSourceURL(ctx, raw)
}

// handleScan reconciles the DB with disk in both directions:
//   - forward: any "done" record whose media file no longer exists is dropped,
//     so history stops claiming files that were deleted out from under it.
//   - reverse: any media file present on disk but absent from the DB is
//     re-imported, with video id / artist / url recovered from its path.
//
// Dropping a file back into the media tree and rescanning brings its row back.
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	records, err := s.DB.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	checked, missing := 0, 0
	for _, rec := range records {
		// Only rows that claim a file on disk are worth reconciling; a download
		// that never produced a file has no path to check.
		if rec.FilePath == "" {
			continue
		}
		checked++
		if _, err := os.Stat(rec.FilePath); errors.Is(err, os.ErrNotExist) {
			if err := s.DB.Delete(r.Context(), rec.VideoID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			missing++
		}
	}

	// Reverse: re-import files on disk that the DB doesn't know about. Run after
	// the forward pass so a moved/renamed file is dropped then re-added cleanly.
	added, err := s.DL.ScanDisk(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{"checked": checked, "missing": missing, "added": added})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
