package api

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"iwaradl-managed/internal/auth"
	"iwaradl-managed/internal/db"
	"iwaradl-managed/internal/downloader"
)

type Server struct {
	DL         *downloader.Downloader
	DB         *db.DB
	Auth       *auth.Manager
	Token      string
	WebDir     string
	ResolveURL func(context.Context, string) (string, error)
}

type authSessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type credentialsRequest struct {
	CurrentPassword string `json:"current_password"`
	Username        string `json:"username"`
	NewPassword     string `json:"new_password"`
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
	// Auth endpoints authorize themselves; they must stay outside s.auth so the
	// login screen can reach them while unauthenticated.
	mux.HandleFunc("/api/auth/session", s.handleAuthSession)
	mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/auth/credentials", s.handleAuthCredentials)
	mux.HandleFunc("/api/downloads/active", s.auth(s.handleActiveDownloads))
	// The completed media file itself, streamed for external clients (never-stelle)
	// and the UI download button. More specific than the "/api/downloads/" subtree
	// below, so Go 1.22's ServeMux routes it here.
	mux.HandleFunc("GET /api/downloads/{video_id}/file", s.auth(s.handleDownloadFile))
	mux.HandleFunc("/api/downloads/", s.auth(s.handleDownloadRecord))
	mux.HandleFunc("/api/downloads", s.auth(s.handleDownloads))
	mux.HandleFunc("/api/history", s.auth(s.handleHistory))
	mux.HandleFunc("/api/counts", s.auth(s.handleCounts))
	mux.HandleFunc("/api/queue", s.auth(s.handleQueue))
	mux.HandleFunc("/api/scan", s.auth(s.handleScan))
	if s.WebDir != "" {
		// Static files (including the login page) are served unauthenticated; the
		// SPA self-gates via /api/auth/session and the /api/* endpoints enforce.
		mux.Handle("/", s.staticHandler())
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

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// authorized accepts either the optional Bearer API token (external/integration
// clients such as never-stelle) or a valid signed session cookie (the bundled
// UI login). Either one grants access to the /api/* business endpoints.
func (s *Server) authorized(r *http.Request) bool {
	if s.Token != "" {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") &&
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(s.Token)) == 1 {
			return true
		}
	}
	if s.Auth != nil {
		if sess, err := s.Auth.SessionFromRequest(r.Context(), r); err == nil && sess != nil {
			return true
		}
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAuthSession reports whether the current request carries a valid session
// cookie, so the SPA can decide between the login screen and the app shell.
func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	sess := s.currentSession(r)
	if sess == nil {
		writeJSON(w, http.StatusOK, authSessionResponse{Authenticated: false})
		return
	}
	writeJSON(w, http.StatusOK, authSessionResponse{Authenticated: true, Username: sess.Username})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.Auth == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth not configured"})
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	settings, err := s.Auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid username or password."})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	token, err := s.Auth.CreateSessionToken(settings)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.Auth.SetSessionCookie(w, token)
	writeJSON(w, http.StatusOK, authSessionResponse{Authenticated: true, Username: settings.Username})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.Auth != nil {
		s.Auth.ClearSessionCookie(w)
	}
	writeJSON(w, http.StatusOK, authSessionResponse{Authenticated: false})
}

func (s *Server) handleAuthCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.Auth == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth not configured"})
		return
	}
	if s.currentSession(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	settings, err := s.Auth.UpdateCredentials(r.Context(), req.CurrentPassword, req.Username, req.NewPassword)
	if err != nil {
		var v *auth.ValidationError
		if errors.As(err, &v) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": v.Message})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// The version bump invalidated the caller's old cookie; hand back a fresh one
	// so the current session stays signed in.
	token, err := s.Auth.CreateSessionToken(settings)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.Auth.SetSessionCookie(w, token)
	writeJSON(w, http.StatusOK, authSessionResponse{Authenticated: true, Username: settings.Username})
}

func (s *Server) currentSession(r *http.Request) *auth.Session {
	if s.Auth == nil {
		return nil
	}
	sess, err := s.Auth.SessionFromRequest(r.Context(), r)
	if err != nil {
		return nil
	}
	return sess
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

// handleDownloadFile streams the completed media file for a video id so external
// clients (never-stelle) and the UI can pull the actual bytes, not just the
// metadata row. Only "done" rows have a file on disk; anything else is 409/404.
// http.ServeContent supplies Content-Type, Content-Length, Range, and
// If-Modified-Since handling, so large files stream and downloads are resumable.
func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	videoID := strings.TrimSpace(r.PathValue("video_id"))
	if videoID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid video id"})
		return
	}

	rec, err := s.DB.Get(r.Context(), videoID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if rec.Status != db.StatusDone || rec.FilePath == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "download is not complete"})
		return
	}
	if !s.mediaPathAllowed(rec.FilePath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "file path is outside the media directory"})
		return
	}

	f, err := os.Open(rec.FilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The row still claims a file that has since vanished; a scan would drop
			// the row. Report it as gone rather than a server error.
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "media file is missing on disk"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "media file is missing on disk"})
		return
	}

	name := filepath.Base(rec.FilePath)
	w.Header().Set("Content-Disposition", contentDisposition(name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// mediaPathAllowed guards against serving a record whose stored path escaped the
// media root. Paths are written by our own downloader/scan, so this is defense
// in depth; it is skipped when the media dir is unknown (e.g. in tests).
func (s *Server) mediaPathAllowed(p string) bool {
	root := ""
	if s.DL != nil {
		root = s.DL.MediaDir
	}
	if root == "" {
		return true
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// contentDisposition builds an attachment header that survives non-ASCII titles:
// a sanitized ASCII fallback for old clients plus an RFC 5987 filename* carrying
// the UTF-8 name percent-encoded.
func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(name))
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
