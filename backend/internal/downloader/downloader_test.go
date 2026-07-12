package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if len(d.queue) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(d.queue))
	}
	oldJob := d.queue[0]
	if oldJob.videoID != "abc123" {
		t.Fatalf("queued videoID = %q, want abc123", oldJob.videoID)
	}
	if d.jobStopped(oldJob) {
		t.Fatal("freshly queued job was marked stopped")
	}

	if err := d.Cancel(ctx, "abc123"); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if len(d.queue) != 0 {
		t.Fatalf("queued jobs after cancel = %d, want 0", len(d.queue))
	}
	if !d.jobStopped(oldJob) {
		t.Fatal("canceled queued job was not marked stopped")
	}

	d.Enqueue(url)
	if len(d.queue) != 1 {
		t.Fatalf("queued jobs after requeue = %d, want 1", len(d.queue))
	}
	newJob := d.queue[0]
	if d.jobStopped(newJob) {
		t.Fatal("newly queued generation was marked stopped")
	}
	if !d.jobStopped(oldJob) {
		t.Fatal("old queued generation became runnable after requeue")
	}
}

func TestOnDemandSchedulerHonorsMaxConcurrencyAndStopsIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan string, 3)
	finished := make(chan struct{}, 3)
	release := make(chan struct{})

	d := &Downloader{MaxConcurrency: 2}
	d.downloadFunc = func(ctx context.Context, url string, generation uint64) error {
		videoID, err := ExtractVideoID(url)
		if err != nil {
			return err
		}
		started <- videoID
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		finished <- struct{}{}
		return nil
	}
	d.Start(ctx)

	d.Enqueue("https://www.iwara.tv/video/one111/one")
	d.Enqueue("https://www.iwara.tv/video/two222/two")
	d.Enqueue("https://www.iwara.tv/video/three333/three")

	waitForStarted(t, started)
	waitForStarted(t, started)
	select {
	case videoID := <-started:
		t.Fatalf("third download %q started before a concurrency slot opened", videoID)
	case <-time.After(100 * time.Millisecond):
	}

	d.mu.RLock()
	running, queued := d.running, len(d.queue)
	d.mu.RUnlock()
	if running != 2 {
		t.Fatalf("running = %d, want 2", running)
	}
	if queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}

	close(release)
	waitForStarted(t, started)
	waitForFinished(t, finished)
	waitForFinished(t, finished)
	waitForFinished(t, finished)

	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.running != 0 {
		t.Fatalf("running after completion = %d, want 0", d.running)
	}
	if len(d.queue) != 0 {
		t.Fatalf("queue after completion = %d, want 0", len(d.queue))
	}
	if d.queue != nil {
		t.Fatal("queue retained backing storage after becoming idle")
	}
	if d.active != nil || d.progress != nil || d.generations != nil || d.canceled != nil {
		t.Fatalf("idle state retained maps: active=%v progress=%v generations=%v canceled=%v",
			d.active != nil, d.progress != nil, d.generations != nil, d.canceled != nil)
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

func waitForStarted(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case videoID := <-started:
		return videoID
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for download to start")
		return ""
	}
}

func waitForFinished(t *testing.T, finished <-chan struct{}) {
	t.Helper()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for download to finish")
	}
}
