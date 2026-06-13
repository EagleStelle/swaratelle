package downloader

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"iwaradl-managed/internal/db"
)

// maxNameBytes is the per-path-component limit on the common Linux filesystems
// (ext4, btrfs) the media volume runs on: 255 *bytes*, not characters. Multibyte
// UTF-8 titles (e.g. CJK at 3 bytes/char) blow past it fast and trip
// ENAMETOOLONG (Errno 36) on save, so every component we create is capped.
const maxNameBytes = 255

var videoIDRe = regexp.MustCompile(`/video/([A-Za-z0-9]+)`)

// progressRe matches iwaradl's status line, which the grab-based downloader
// redraws in place, e.g. "[ 26.90%]   76 MB/  284 MB  /path/file.mp4".
var progressRe = regexp.MustCompile(`\[\s*([0-9]+(?:\.[0-9]+)?)%\]`)

// filenameIDRe pulls the video id out of a saved filename produced by the
// "{{title}} [{{video_id}}]" template, e.g. "67. Zani Jennie SOLO [rmdYMy2kS25ybx]".
// Group 1 is the title, group 2 the id (last bracketed token).
var filenameIDRe = regexp.MustCompile(`^(.*) \[([A-Za-z0-9]+)\]$`)

var stateFiles = map[string]bool{
	"jobs.list":    true,
	"history.list": true,
}

var mediaExts = map[string]bool{
	".mp4":  true,
	".mkv":  true,
	".webm": true,
	".mov":  true,
	".avi":  true,
	".m4v":  true,
}

type Downloader struct {
	BinaryPath string
	ScratchDir string
	MediaDir   string
	Database   *db.DB

	mu sync.RWMutex

	// progress holds the live percent (0-100) of in-flight downloads, keyed by
	// video id. It is transient -- never persisted -- and surfaced through the
	// API by overlaying it onto "downloading" rows.
	progress map[string]int

	// generations gives each queued item a small identity token. Canceling a
	// pending row marks the current generation as stopped, so an old URL still
	// buffered in jobs cannot recreate the deleted DB row later.
	generations map[string]uint64
	canceled    map[string]uint64
	active      map[string]*activeDownload

	// jobs is the work queue fed by Enqueue and drained by StartWorker, so the
	// HTTP handler returns immediately instead of blocking on the download.
	jobs chan downloadJob
}

type downloadJob struct {
	url        string
	videoID    string
	generation uint64
}

type activeDownload struct {
	cancel     context.CancelFunc
	done       chan struct{}
	generation uint64
}

// StartWorker drains the job queue, running one download at a time with the
// given long-lived context (not a request context, so downloads outlive the
// HTTP call that queued them). Call once at startup.
func (d *Downloader) StartWorker(ctx context.Context) {
	jobs := d.ensureJobs()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-jobs:
				if d.jobStopped(job) || !d.jobStillWanted(ctx, job) {
					continue
				}
				if err := d.downloadOne(ctx, job.url, job.generation); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("download %s: %v", job.url, err)
				}
			}
		}
	}()
}

// Enqueue adds a URL to the work queue. It never blocks the caller: if the
// buffer is somehow full, the URL is dropped (it stays queryable as a pending
// row and a later re-queue will pick it up).
func (d *Downloader) Enqueue(url string) {
	jobs := d.ensureJobs()
	job := downloadJob{url: url}
	if videoID, err := ExtractVideoID(url); err == nil {
		job.videoID = videoID
		d.mu.Lock()
		if d.generations == nil {
			d.generations = make(map[string]uint64)
		}
		d.generations[videoID]++
		job.generation = d.generations[videoID]
		d.mu.Unlock()
	}
	select {
	case jobs <- job:
	default:
		log.Printf("queue full, dropping %s", url)
	}
}

// Cancel stops queued or currently running work for videoID. For pending jobs it
// records the current generation as canceled so the worker skips the buffered
// job later; for active jobs it also cancels the process context and waits until
// DownloadOne has run its cleanup defers.
func (d *Downloader) Cancel(ctx context.Context, videoID string) error {
	var active *activeDownload

	d.mu.Lock()
	if d.canceled == nil {
		d.canceled = make(map[string]uint64)
	}
	generation := d.generations[videoID]
	if current := d.active[videoID]; current != nil {
		active = current
		if current.generation > generation {
			generation = current.generation
		}
	}
	d.canceled[videoID] = generation
	d.mu.Unlock()

	if active == nil {
		return nil
	}
	active.cancel()
	select {
	case <-active.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Downloader) ensureJobs() chan downloadJob {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.jobs == nil {
		d.jobs = make(chan downloadJob, 1024)
	}
	return d.jobs
}

func (d *Downloader) jobStillWanted(ctx context.Context, job downloadJob) bool {
	if d.Database == nil || job.videoID == "" {
		return true
	}
	rec, err := d.Database.Get(ctx, job.videoID)
	if err != nil {
		log.Printf("load queued download %s: %v", job.videoID, err)
		return false
	}
	if rec == nil || rec.Status == db.StatusDone {
		return false
	}
	return true
}

func (d *Downloader) jobStopped(job downloadJob) bool {
	if job.videoID == "" {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.jobStoppedLocked(job.videoID, job.generation)
}

func (d *Downloader) jobStoppedLocked(videoID string, generation uint64) bool {
	if generation > 0 {
		if current, ok := d.generations[videoID]; ok && current != generation {
			return true
		}
	}
	if canceled, ok := d.canceled[videoID]; ok {
		if generation == 0 {
			return canceled == 0
		}
		return canceled >= generation
	}
	return false
}

func (d *Downloader) registerActive(videoID string, generation uint64, active *activeDownload) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.jobStoppedLocked(videoID, generation) {
		return false
	}
	if d.active == nil {
		d.active = make(map[string]*activeDownload)
	}
	d.active[videoID] = active
	return true
}

func (d *Downloader) unregisterActive(videoID string, active *activeDownload) {
	d.mu.Lock()
	if d.active[videoID] == active {
		delete(d.active, videoID)
	}
	d.mu.Unlock()
}

func (d *Downloader) setProgress(videoID string, pct int) {
	d.mu.Lock()
	if d.progress == nil {
		d.progress = make(map[string]int)
	}
	d.progress[videoID] = pct
	d.mu.Unlock()
}

func (d *Downloader) clearProgress(videoID string) {
	d.mu.Lock()
	delete(d.progress, videoID)
	d.mu.Unlock()
}

// Progress returns the live percent for a video id, and whether one is known.
func (d *Downloader) Progress(videoID string) (int, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	p, ok := d.progress[videoID]
	return p, ok
}

func ExtractVideoID(url string) (string, error) {
	m := videoIDRe.FindStringSubmatch(url)
	if len(m) < 2 {
		return "", fmt.Errorf("could not extract video id from url: %s", url)
	}
	return m[1], nil
}

func (d *Downloader) DownloadOne(ctx context.Context, url string) error {
	return d.downloadOne(ctx, url, 0)
}

func (d *Downloader) downloadOne(ctx context.Context, url string, generation uint64) error {
	videoID, err := ExtractVideoID(url)
	if err != nil {
		return err
	}
	if d.jobStopped(downloadJob{videoID: videoID, generation: generation}) {
		return context.Canceled
	}

	done, err := d.Database.IsDone(ctx, videoID)
	if err != nil {
		return fmt.Errorf("dedup check: %w", err)
	}
	if done {
		return nil
	}

	downloadCtx, cancel := context.WithCancel(ctx)
	active := &activeDownload{
		cancel:     cancel,
		done:       make(chan struct{}),
		generation: generation,
	}
	if !d.registerActive(videoID, generation, active) {
		cancel()
		return context.Canceled
	}
	defer func() {
		d.unregisterActive(videoID, active)
		close(active.done)
		cancel()
	}()
	if downloadCtx.Err() != nil {
		return d.cancelDownload(ctx, videoID)
	}

	if err := d.Database.MarkDownloading(ctx, videoID, url); err != nil {
		return fmt.Errorf("mark downloading: %w", err)
	}
	if downloadCtx.Err() != nil {
		return d.cancelDownload(ctx, videoID)
	}

	workDir, err := os.MkdirTemp(d.ScratchDir, videoID+"-")
	if err != nil {
		_ = d.Database.MarkFailed(ctx, videoID, err.Error())
		return err
	}
	defer os.RemoveAll(workDir)
	// Whatever happens, this download is no longer in flight once we return.
	defer d.clearProgress(videoID)
	d.setProgress(videoID, 0)

	// iwaradl names the scratch file "<title> [<id>].<ext>" before/while writing
	// bytes, so poll for it to learn the real title and surface it mid-download.
	titleStop := make(chan struct{})
	defer close(titleStop)
	go d.captureTitle(downloadCtx, workDir, videoID, titleStop)

	// No config file: iwaradl runs anonymously and every setting has a flag.
	// --use-sub-dir puts files under an "<artist>/" folder, and the template
	// names each file "<title> [<video_id>]". moveMedia preserves that layout.
	cmd := exec.CommandContext(downloadCtx, d.BinaryPath,
		"--root-dir", workDir,
		"--use-sub-dir",
		"--filename-template", "{{title}} [{{video_id}}]",
		"--thread-num", "4",
		"--max-retry", "3",
		url,
	)
	out, runErr := d.runWithProgress(cmd, videoID)
	if downloadCtx.Err() != nil {
		return d.cancelDownload(ctx, videoID)
	}
	if runErr != nil {
		msg := fmt.Sprintf("%v: %s", runErr, truncate(out, 2000))
		_ = d.Database.MarkFailed(ctx, videoID, "iwaradl run: "+msg)
		return fmt.Errorf("iwaradl run: %s", msg)
	}
	d.setProgress(videoID, 100)

	finalPath, title, artist, size, err := d.moveMedia(workDir, videoID)
	if err != nil {
		_ = d.Database.MarkFailed(ctx, videoID, err.Error())
		return err
	}
	if downloadCtx.Err() != nil {
		_ = os.Remove(finalPath)
		return d.cancelDownload(ctx, videoID)
	}

	if err := d.Database.MarkDone(ctx, videoID, finalPath, title, artist, size); err != nil {
		return fmt.Errorf("mark done: %w", err)
	}
	return nil
}

func (d *Downloader) cancelDownload(ctx context.Context, videoID string) error {
	_ = d.Database.Delete(ctx, videoID)
	return context.Canceled
}

// captureTitle polls the scratch dir for the media file iwaradl creates and,
// once found, records its title so the UI stops showing a bare video id. It
// stops on the first success, when the download ends (stop), or on ctx cancel.
func (d *Downloader) captureTitle(ctx context.Context, workDir, videoID string, stop <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			if title, ok := findTitleInDir(workDir); ok {
				_ = d.Database.SetTitle(ctx, videoID, title)
				return
			}
		}
	}
}

// findTitleInDir returns the parsed title of the first media file under dir.
func findTitleInDir(dir string) (string, bool) {
	var title string
	found := false
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found {
			return nil
		}
		base := filepath.Base(p)
		if stateFiles[strings.ToLower(base)] {
			return nil
		}
		if mediaExts[strings.ToLower(filepath.Ext(base))] {
			title = parseTitle(base)
			found = true
		}
		return nil
	})
	return title, found
}

// runWithProgress runs cmd with stdout+stderr merged into one stream, parsing
// iwaradl's redrawn "[ NN.NN%]" status line to publish live progress for
// videoID, while keeping a capped copy of the output for error reporting.
func (d *Downloader) runWithProgress(cmd *exec.Cmd, videoID string) (string, error) {
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	var captured bytes.Buffer
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		// iwaradl redraws in place with carriage returns and ANSI escapes rather
		// than newlines, so split on either \n or \r to see each update.
		sc.Split(scanLinesAndCR)
		for sc.Scan() {
			line := sc.Text()
			if captured.Len() < 64*1024 {
				captured.WriteString(line)
				captured.WriteByte('\n')
			}
			if pct, ok := parsePercent(line); ok {
				d.setProgress(videoID, pct)
			}
		}
		// If the scanner stopped on an error (e.g. an over-long token), keep
		// draining the pipe so the process isn't blocked writing into it.
		if sc.Err() != nil {
			_, _ = io.Copy(io.Discard, pr)
		}
	}()

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		<-scanDone
		return captured.String(), err
	}
	runErr := cmd.Wait()
	// Closing the write end ends the scanner's read loop; then wait for it.
	_ = pw.Close()
	<-scanDone
	return captured.String(), runErr
}

// scanLinesAndCR is a bufio.SplitFunc that breaks on either '\n' or '\r', so an
// in-place progress bar (which uses carriage returns) yields one token per
// redraw instead of buffering until EOF.
func scanLinesAndCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// parsePercent extracts the integer percent from an iwaradl status line, e.g.
// "[ 26.90%] ..." -> 26. Returns false when the line carries no percent.
func parsePercent(line string) (int, bool) {
	m := progressRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	pct := int(f)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// ScanDisk walks MediaDir and re-imports any media file whose video id can be
// recovered from its filename but which is absent from the database. This is the
// reverse of the "delete rows whose file vanished" reconcile: drop a file back
// into the media tree and a scan brings its row back, with artist/url/title
// reconstructed from the "<artist>/<title> [<video_id>].<ext>" layout on disk.
func (d *Downloader) ScanDisk(ctx context.Context) (int, error) {
	added := 0
	err := filepath.Walk(d.MediaDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if stateFiles[strings.ToLower(base)] {
			return nil
		}
		if !mediaExts[strings.ToLower(filepath.Ext(base))] {
			return nil
		}

		name := strings.TrimSuffix(base, filepath.Ext(base))
		m := filenameIDRe.FindStringSubmatch(name)
		if m == nil {
			// No recoverable id in the name -- can't dedup it, leave it alone.
			return nil
		}
		videoID := m[2]

		// Artist is the top folder under MediaDir; empty if the file sits at root.
		artist := ""
		if rel, relErr := filepath.Rel(d.MediaDir, path); relErr == nil {
			if dir := filepath.Dir(rel); dir != "." {
				artist = strings.Split(filepath.ToSlash(dir), "/")[0]
			}
		}

		title := m[1]
		url := "https://www.iwara.tv/video/" + videoID
		inserted, insErr := d.Database.InsertReconciled(ctx, videoID, url, path, title, artist, info.Size())
		if insErr != nil {
			return insErr
		}
		if inserted {
			added++
		}
		return nil
	})
	if err != nil {
		return added, fmt.Errorf("scan media dir: %w", err)
	}
	return added, nil
}

func (d *Downloader) moveMedia(workDir, videoID string) (path, title, artist string, size int64, err error) {
	var found []string
	err = filepath.Walk(workDir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if stateFiles[strings.ToLower(base)] {
			return nil
		}
		if mediaExts[strings.ToLower(filepath.Ext(base))] {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		return "", "", "", 0, fmt.Errorf("scan workdir: %w", err)
	}
	if len(found) == 0 {
		return "", "", "", 0, fmt.Errorf("no media file produced for %s", videoID)
	}

	src := found[0]
	// Preserve iwaradl's "<artist>/<title> [<video_id>].<ext>" layout: take the
	// path relative to the scratch root and recreate it under MediaDir. Every
	// component is length-capped so an over-long title can't trip ENAMETOOLONG.
	rel, err := filepath.Rel(workDir, src)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("relpath: %w", err)
	}

	dir, base := filepath.Split(rel)
	parts := strings.Split(filepath.ToSlash(strings.TrimSuffix(dir, string(filepath.Separator))), "/")
	dst0 := d.MediaDir
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		dst0 = filepath.Join(dst0, capName(p))
	}
	dst := filepath.Join(dst0, shortenFilename(base))

	// Title is the filename minus the "[<video_id>]" suffix and extension.
	title = parseTitle(base)

	// The top folder of that relative layout is the artist; empty if iwaradl
	// dropped the media at the scratch root with no artist subfolder.
	if d := filepath.Dir(rel); d != "." {
		artist = capName(strings.Split(filepath.ToSlash(d), "/")[0])
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", "", "", 0, fmt.Errorf("create artist dir: %w", err)
	}

	size, err = moveFile(src, dst)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("move media: %w", err)
	}
	return dst, title, artist, size, nil
}

// parseTitle returns the human title from a "<title> [<video_id>].<ext>"
// filename, falling back to the bare name when there is no id suffix.
func parseTitle(base string) string {
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if m := filenameIDRe.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return name
}

func moveFile(src, dst string) (int64, error) {
	if err := os.Rename(src, dst); err == nil {
		info, statErr := os.Stat(dst)
		if statErr != nil {
			return 0, statErr
		}
		return info.Size(), nil
	}

	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	size, err := io.Copy(out, in)
	if err != nil {
		return 0, err
	}
	if err := out.Sync(); err != nil {
		return 0, err
	}
	_ = os.Remove(src)
	return size, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// shortenFilename caps a filename to maxNameBytes while keeping the extension
// and the trailing "[video_id]" token intact -- only the title is trimmed, so
// the file is still recognizable and ScanDisk can still recover its id.
func shortenFilename(base string) string {
	if len(base) <= maxNameBytes {
		return base
	}
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	suffix := ""
	if m := filenameIDRe.FindStringSubmatch(name); m != nil {
		suffix = " [" + m[2] + "]"
		name = m[1]
	}

	budget := maxNameBytes - len(ext) - len(suffix)
	if budget < 0 {
		budget = 0
	}
	return truncateBytes(name, budget) + suffix + ext
}

// capName caps a single path component (e.g. an artist folder) to maxNameBytes.
func capName(s string) string {
	if len(s) <= maxNameBytes {
		return s
	}
	return truncateBytes(s, maxNameBytes)
}

// truncateBytes cuts s to at most n bytes, backing off to a UTF-8 rune boundary
// so a multibyte character is never split, then trims trailing spaces.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " ")
}
