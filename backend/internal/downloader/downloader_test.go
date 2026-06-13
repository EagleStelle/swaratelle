package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"iwaradl-managed/internal/db"
)

func TestScanDiskReimportsMediaFileRemovedFromDB(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mediaDir := filepath.Join(root, "media")
	artistDir := filepath.Join(mediaDir, "Trace Artist")
	if err := os.MkdirAll(artistDir, 0o700); err != nil {
		t.Fatal(err)
	}

	videoID := "abc123XYZ"
	title := "Traceable Title"
	filePath := filepath.Join(artistDir, title+" ["+videoID+"].mp4")
	content := []byte("video bytes")
	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(filepath.Join(root, "downloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	d := &Downloader{
		MediaDir: mediaDir,
		Database: database,
	}

	added, err := d.ScanDisk(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}

	rec, err := database.Get(ctx, videoID)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("expected reimported record")
	}
	if rec.Status != db.StatusDone {
		t.Fatalf("Status = %q, want %q", rec.Status, db.StatusDone)
	}
	if rec.SourceURL != "https://www.iwara.tv/video/"+videoID {
		t.Fatalf("SourceURL = %q", rec.SourceURL)
	}
	if rec.FilePath != filePath {
		t.Fatalf("FilePath = %q, want %q", rec.FilePath, filePath)
	}
	if rec.FileSize != int64(len(content)) {
		t.Fatalf("FileSize = %d, want %d", rec.FileSize, len(content))
	}
	if rec.Title != title {
		t.Fatalf("Title = %q, want %q", rec.Title, title)
	}
	if rec.Artist != "Trace Artist" {
		t.Fatalf("Artist = %q, want Trace Artist", rec.Artist)
	}
}

func TestDownloadOneKeepsFailedRecordWithError(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	scratchFile := filepath.Join(root, "scratch-file")
	if err := os.WriteFile(scratchFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(filepath.Join(root, "downloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	d := &Downloader{
		ScratchDir: scratchFile,
		Database:   database,
	}

	err = d.DownloadOne(ctx, "https://www.iwara.tv/video/abc123/failing-video")
	if err == nil {
		t.Fatal("DownloadOne returned nil error, want failure")
	}

	rec, err := database.Get(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("expected failed record to remain")
	}
	if rec.Status != db.StatusFailed {
		t.Fatalf("Status = %q, want %q", rec.Status, db.StatusFailed)
	}
	if rec.Error == "" {
		t.Fatal("expected error message to be recorded")
	}
}
