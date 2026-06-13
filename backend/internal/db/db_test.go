package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestListActiveIncludesPendingDownloadingAndFailed(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	insertTestRecord(t, database, "done-1", StatusDone, 100)
	insertTestRecord(t, database, "downloading-1", StatusDownloading, 300)
	insertTestRecord(t, database, "pending-1", StatusPending, 200)
	insertTestRecord(t, database, "failed-1", StatusFailed, 400)

	records, err := database.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive returned error: %v", err)
	}
	if got, want := len(records), 3; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	if got, want := recordIDs(records), []string{"failed-1", "downloading-1", "pending-1"}; !sameStrings(got, want) {
		t.Fatalf("active order = %v, want %v", got, want)
	}
}

func TestListHistoryPaginatesWithStableCursor(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	insertTestRecord(t, database, "pending-1", StatusPending, 500)
	insertTestRecord(t, database, "done-c", StatusDone, 400)
	insertTestRecord(t, database, "done-b", StatusDone, 400)
	insertTestRecord(t, database, "failed-a", StatusFailed, 300)
	insertTestRecord(t, database, "downloading-1", StatusDownloading, 200)
	insertTestRecord(t, database, "done-old", StatusDone, 100)

	firstPage, err := database.ListHistory(ctx, 2, nil, "")
	if err != nil {
		t.Fatalf("ListHistory first page returned error: %v", err)
	}
	if got, want := recordIDs(firstPage), []string{"done-c", "done-b"}; !sameStrings(got, want) {
		t.Fatalf("first page = %v, want %v", got, want)
	}

	nextPage, err := database.ListHistory(ctx, 2, &PageCursor{
		UpdatedAt: firstPage[len(firstPage)-1].UpdatedAt,
		VideoID:   firstPage[len(firstPage)-1].VideoID,
	}, "")
	if err != nil {
		t.Fatalf("ListHistory next page returned error: %v", err)
	}
	if got, want := recordIDs(nextPage), []string{"done-old"}; !sameStrings(got, want) {
		t.Fatalf("next page = %v, want %v", got, want)
	}
}

func TestListHistorySearchesTitleAndArtist(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	insertTestRecordWithMetadata(t, database, "title-match", StatusDone, 500, "Soft Light Study", "Kana")
	insertTestRecordWithMetadata(t, database, "artist-match", StatusDone, 400, "Morning", "Aster Vale")
	insertTestRecordWithMetadata(t, database, "active-match", StatusPending, 300, "Soft Pending", "Aster Vale")
	insertTestRecordWithMetadata(t, database, "failed-match", StatusFailed, 250, "Soft Failed", "Aster Vale")
	insertTestRecordWithMetadata(t, database, "miss", StatusDone, 200, "Night Road", "Mira")

	titleMatches, err := database.ListHistory(ctx, 10, nil, "soft")
	if err != nil {
		t.Fatalf("ListHistory title search returned error: %v", err)
	}
	if got, want := recordIDs(titleMatches), []string{"title-match"}; !sameStrings(got, want) {
		t.Fatalf("title search = %v, want %v", got, want)
	}

	artistMatches, err := database.ListHistory(ctx, 10, nil, "ASTER")
	if err != nil {
		t.Fatalf("ListHistory artist search returned error: %v", err)
	}
	if got, want := recordIDs(artistMatches), []string{"artist-match"}; !sameStrings(got, want) {
		t.Fatalf("artist search = %v, want %v", got, want)
	}
}

func TestListHistorySearchPaginates(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	insertTestRecordWithMetadata(t, database, "song-c", StatusDone, 500, "Song C", "Artist")
	insertTestRecordWithMetadata(t, database, "skip", StatusDone, 450, "Talk", "Artist")
	insertTestRecordWithMetadata(t, database, "song-b", StatusDone, 400, "Song B", "Artist")
	insertTestRecordWithMetadata(t, database, "song-a", StatusFailed, 300, "Song A", "Artist")

	firstPage, err := database.ListHistory(ctx, 2, nil, "song")
	if err != nil {
		t.Fatalf("ListHistory search first page returned error: %v", err)
	}
	if got, want := recordIDs(firstPage), []string{"song-c", "song-b"}; !sameStrings(got, want) {
		t.Fatalf("search first page = %v, want %v", got, want)
	}

	nextPage, err := database.ListHistory(ctx, 2, &PageCursor{
		UpdatedAt: firstPage[len(firstPage)-1].UpdatedAt,
		VideoID:   firstPage[len(firstPage)-1].VideoID,
	}, "song")
	if err != nil {
		t.Fatalf("ListHistory search next page returned error: %v", err)
	}
	if got, want := recordIDs(nextPage), []string{}; !sameStrings(got, want) {
		t.Fatalf("search next page = %v, want %v", got, want)
	}
}

func TestMarkFailedRecordsErrorAndMarkPendingClearsIt(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	if err := database.MarkPending(ctx, "retry-1", "https://example.com/retry-1"); err != nil {
		t.Fatalf("MarkPending returned error: %v", err)
	}
	if err := database.MarkFailed(ctx, "retry-1", "network unavailable"); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}
	rec, err := database.Get(ctx, "retry-1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected failed record")
	}
	if rec.Status != StatusFailed {
		t.Fatalf("Status = %q, want %q", rec.Status, StatusFailed)
	}
	if rec.Error != "network unavailable" {
		t.Fatalf("Error = %q, want network unavailable", rec.Error)
	}

	if err := database.MarkPending(ctx, "retry-1", "https://example.com/retry-1"); err != nil {
		t.Fatalf("MarkPending retry returned error: %v", err)
	}
	rec, err = database.Get(ctx, "retry-1")
	if err != nil {
		t.Fatalf("Get retry returned error: %v", err)
	}
	if rec.Status != StatusPending {
		t.Fatalf("Status after retry = %q, want %q", rec.Status, StatusPending)
	}
	if rec.Error != "" {
		t.Fatalf("Error after retry = %q, want empty", rec.Error)
	}
}

func TestListHistorySearchEscapesLikeWildcards(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	insertTestRecordWithMetadata(t, database, "literal-percent", StatusDone, 500, "100% Real", "")
	insertTestRecordWithMetadata(t, database, "percent-decoy", StatusDone, 400, "100X Real", "")
	insertTestRecordWithMetadata(t, database, "literal-underscore", StatusDone, 300, "part_two", "")
	insertTestRecordWithMetadata(t, database, "underscore-decoy", StatusDone, 200, "partxtwo", "")

	percentMatches, err := database.ListHistory(ctx, 10, nil, "%")
	if err != nil {
		t.Fatalf("ListHistory percent search returned error: %v", err)
	}
	if got, want := recordIDs(percentMatches), []string{"literal-percent"}; !sameStrings(got, want) {
		t.Fatalf("percent search = %v, want %v", got, want)
	}

	underscoreMatches, err := database.ListHistory(ctx, 10, nil, "_")
	if err != nil {
		t.Fatalf("ListHistory underscore search returned error: %v", err)
	}
	if got, want := recordIDs(underscoreMatches), []string{"literal-underscore"}; !sameStrings(got, want) {
		t.Fatalf("underscore search = %v, want %v", got, want)
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "downloads.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	return database
}

func insertTestRecord(t *testing.T, database *DB, videoID string, status Status, updatedAt int64) {
	t.Helper()
	insertTestRecordWithMetadata(t, database, videoID, status, updatedAt, "", "")
}

func insertTestRecordWithMetadata(t *testing.T, database *DB, videoID string, status Status, updatedAt int64, title, artist string) {
	t.Helper()
	_, err := database.conn.ExecContext(context.Background(),
		`INSERT INTO downloads (video_id, source_url, status, title, artist, attempts, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		videoID, "https://example.com/"+videoID, string(status), title, artist, updatedAt, updatedAt)
	if err != nil {
		t.Fatalf("insert %s returned error: %v", videoID, err)
	}
}

func recordIDs(records []Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.VideoID)
	}
	return ids
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
