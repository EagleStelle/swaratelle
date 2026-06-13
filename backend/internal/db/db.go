package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS downloads (
    video_id      TEXT PRIMARY KEY,
    source_url    TEXT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('pending','downloading','done','failed')),
    file_path     TEXT,
    file_size     INTEGER,
    title         TEXT,
    artist        TEXT,
    error         TEXT,
    attempts      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_downloads_status ON downloads(status);
CREATE INDEX IF NOT EXISTS idx_downloads_updated ON downloads(updated_at DESC, video_id DESC);
CREATE INDEX IF NOT EXISTS idx_downloads_status_updated ON downloads(status, updated_at DESC, video_id DESC);
CREATE INDEX IF NOT EXISTS idx_downloads_active_order
ON downloads(updated_at DESC, video_id DESC)
WHERE status IN ('pending','downloading','failed');
CREATE INDEX IF NOT EXISTS idx_downloads_history_order
ON downloads(updated_at DESC, video_id DESC)
WHERE status = 'done';
`

type Status string

const (
	StatusPending     Status = "pending"
	StatusDownloading Status = "downloading"
	StatusDone        Status = "done"
	StatusFailed      Status = "failed"
)

type Record struct {
	VideoID   string
	SourceURL string
	Status    Status
	FilePath  string
	FileSize  int64
	Title     string
	Artist    string
	Error     string
	Attempts  int
	CreatedAt int64
	UpdatedAt int64

	// Progress is the live download percent (0-100). It is not stored in the DB;
	// the API overlays it onto "downloading" rows from the downloader's in-memory
	// state, so it is always 0 when read straight from List/Get.
	Progress int
}

type PageCursor struct {
	UpdatedAt int64
	VideoID   string
}

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(context.Background(), schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &DB{conn: conn}, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) IsDone(ctx context.Context, videoID string) (bool, error) {
	var status string
	err := d.conn.QueryRowContext(ctx,
		`SELECT status FROM downloads WHERE video_id = ?`, videoID).Scan(&status)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return Status(status) == StatusDone, nil
}

// MarkPending records a queued download so it shows up immediately, before the
// background worker picks it up. A finished or in-flight row is left as-is; a
// previously failed one is reset to pending so it can be retried.
func (d *DB) MarkPending(ctx context.Context, videoID, url string) error {
	now := time.Now().Unix()
	_, err := d.conn.ExecContext(ctx,
		`INSERT INTO downloads (video_id, source_url, status, attempts, created_at, updated_at)
		 VALUES (?, ?, 'pending', 0, ?, ?)
		 ON CONFLICT(video_id) DO UPDATE SET
		   status=CASE WHEN downloads.status='failed' THEN 'pending' ELSE downloads.status END,
		   source_url=excluded.source_url,
		   error=CASE WHEN downloads.status='failed' THEN NULL ELSE downloads.error END,
		   updated_at=excluded.updated_at`,
		videoID, url, now, now)
	return err
}

func (d *DB) MarkDownloading(ctx context.Context, videoID, url string) error {
	now := time.Now().Unix()
	_, err := d.conn.ExecContext(ctx,
		`INSERT INTO downloads (video_id, source_url, status, attempts, created_at, updated_at)
		 VALUES (?, ?, 'downloading', 1, ?, ?)
		 ON CONFLICT(video_id) DO UPDATE SET
		   status='downloading',
		   error=NULL,
		   attempts=attempts+1,
		   updated_at=excluded.updated_at`,
		videoID, url, now, now)
	return err
}

// SetTitle records the video title as soon as it is known (parsed from the
// filename iwaradl creates), so the UI can show it mid-download.
func (d *DB) SetTitle(ctx context.Context, videoID, title string) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE downloads SET title=? WHERE video_id=?`, title, videoID)
	return err
}

func (d *DB) MarkDone(ctx context.Context, videoID, filePath, title, artist string, size int64) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE downloads SET status='done', file_path=?, file_size=?, title=?, artist=?, error=NULL, updated_at=?
		 WHERE video_id=?`,
		filePath, size, title, artist, time.Now().Unix(), videoID)
	return err
}

func (d *DB) MarkFailed(ctx context.Context, videoID, message string) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE downloads SET status='failed', error=?, updated_at=?
		 WHERE video_id=?`,
		message, time.Now().Unix(), videoID)
	return err
}

// InsertReconciled re-adds a "done" row recovered from disk during a scan. It
// only inserts when the video_id is absent (ON CONFLICT DO NOTHING), so an
// existing record is never clobbered. Returns true when a row was added.
func (d *DB) InsertReconciled(ctx context.Context, videoID, url, filePath, title, artist string, size int64) (bool, error) {
	now := time.Now().Unix()
	res, err := d.conn.ExecContext(ctx,
		`INSERT INTO downloads (video_id, source_url, status, file_path, file_size, title, artist, attempts, created_at, updated_at)
		 VALUES (?, ?, 'done', ?, ?, ?, ?, 0, ?, ?)
		 ON CONFLICT(video_id) DO NOTHING`,
		videoID, url, filePath, size, title, artist, now, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (d *DB) Delete(ctx context.Context, videoID string) error {
	_, err := d.conn.ExecContext(ctx,
		`DELETE FROM downloads WHERE video_id = ?`, videoID)
	return err
}

func (d *DB) List(ctx context.Context) ([]Record, error) {
	return d.queryRecords(ctx,
		`SELECT video_id, source_url, status, COALESCE(file_path,''),
		        COALESCE(file_size,0), COALESCE(title,''), COALESCE(artist,''), COALESCE(error,''), attempts, created_at, updated_at
		 FROM downloads
		 ORDER BY updated_at DESC, video_id DESC`)
}

func (d *DB) ListActive(ctx context.Context) ([]Record, error) {
	return d.queryRecords(ctx,
		`SELECT video_id, source_url, status, COALESCE(file_path,''),
		        COALESCE(file_size,0), COALESCE(title,''), COALESCE(artist,''), COALESCE(error,''), attempts, created_at, updated_at
		 FROM downloads
		 WHERE status IN ('pending','downloading','failed')
		 ORDER BY updated_at DESC, video_id DESC`)
}

func (d *DB) ListHistory(ctx context.Context, limit int, after *PageCursor, search string) ([]Record, error) {
	if limit < 1 {
		return []Record{}, nil
	}

	query := `SELECT video_id, source_url, status, COALESCE(file_path,''),
		        COALESCE(file_size,0), COALESCE(title,''), COALESCE(artist,''), COALESCE(error,''), attempts, created_at, updated_at
		 FROM downloads
		 WHERE status = 'done'`
	args := []any{}
	if search = strings.TrimSpace(search); search != "" {
		pattern := historySearchPattern(search)
		query += ` AND (
		   LOWER(COALESCE(title,'')) LIKE ? ESCAPE '\'
		   OR LOWER(COALESCE(artist,'')) LIKE ? ESCAPE '\'
		 )`
		args = append(args, pattern, pattern)
	}
	if after != nil {
		query += ` AND (updated_at < ? OR (updated_at = ? AND video_id < ?))`
		args = append(args, after.UpdatedAt, after.UpdatedAt, after.VideoID)
	}
	query += ` ORDER BY updated_at DESC, video_id DESC LIMIT ?`
	args = append(args, limit)

	return d.queryRecords(ctx, query, args...)
}

func historySearchPattern(search string) string {
	var b strings.Builder
	b.Grow(len(search) + 2)
	b.WriteByte('%')
	for _, r := range strings.ToLower(search) {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('%')
	return b.String()
}

func (d *DB) queryRecords(ctx context.Context, query string, args ...any) ([]Record, error) {
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Record{}
	for rows.Next() {
		var r Record
		var status string
		if err := rows.Scan(&r.VideoID, &r.SourceURL, &status, &r.FilePath,
			&r.FileSize, &r.Title, &r.Artist, &r.Error, &r.Attempts, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Status = Status(status)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) Get(ctx context.Context, videoID string) (*Record, error) {
	var r Record
	var status string
	err := d.conn.QueryRowContext(ctx,
		`SELECT video_id, source_url, status, COALESCE(file_path,''),
		        COALESCE(file_size,0), COALESCE(title,''), COALESCE(artist,''), COALESCE(error,''), attempts, created_at, updated_at
		 FROM downloads WHERE video_id = ?`, videoID).Scan(
		&r.VideoID, &r.SourceURL, &status, &r.FilePath,
		&r.FileSize, &r.Title, &r.Artist, &r.Error, &r.Attempts, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Status = Status(status)
	return &r, nil
}
