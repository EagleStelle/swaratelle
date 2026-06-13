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

func TestCancelStopsOldQueuedGeneration(t *testing.T) {
	ctx := context.Background()
	d := &Downloader{}
	url := "https://www.iwara.tv/video/abc123/queued-video"

	d.Enqueue(url)
	oldJob := <-d.jobs
	if oldJob.videoID != "abc123" {
		t.Fatalf("queued videoID = %q, want abc123", oldJob.videoID)
	}
	if d.jobStopped(oldJob) {
		t.Fatal("freshly queued job was marked stopped")
	}

	if err := d.Cancel(ctx, "abc123"); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if !d.jobStopped(oldJob) {
		t.Fatal("canceled queued job was not marked stopped")
	}

	d.Enqueue(url)
	newJob := <-d.jobs
	if d.jobStopped(newJob) {
		t.Fatal("newly queued generation was marked stopped")
	}
	if !d.jobStopped(oldJob) {
		t.Fatal("old queued generation became runnable after requeue")
	}
}

func TestCancelStopsActiveDownload(t *testing.T) {
	ctx := context.Background()
	d := &Downloader{
		generations: map[string]uint64{"abc123": 1},
	}
	activeCtx, activeCancel := context.WithCancel(ctx)
	active := &activeDownload{
		cancel:     activeCancel,
		done:       make(chan struct{}),
		generation: 1,
	}
	if !d.registerActive("abc123", 1, active) {
		t.Fatal("registerActive returned false")
	}
	go func() {
		<-activeCtx.Done()
		d.unregisterActive("abc123", active)
		close(active.done)
	}()

	if err := d.Cancel(ctx, "abc123"); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if activeCtx.Err() == nil {
		t.Fatal("active context was not canceled")
	}
	if !d.jobStopped(downloadJob{videoID: "abc123", generation: 1}) {
		t.Fatal("active generation was not marked stopped")
	}
}
