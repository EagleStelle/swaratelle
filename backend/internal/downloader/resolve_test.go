package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinkResolverLeavesIwaraURLUnchanged(t *testing.T) {
	raw := "https://www.iwara.tv/video/CNCQNZEfKO8QYI/mmdparty-tonight-jane-doe"

	got, err := LinkResolver{}.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got != raw {
		t.Fatalf("Resolve = %q, want %q", got, raw)
	}
}

func TestExtractIwaraURLFromOrenoHTML(t *testing.T) {
	html := `<figure class="video-figure">
		<a href="https://www.iwara.tv/video/CNCQNZEfKO8QYI/mmdparty-tonight-jane-doe" target="_blank" rel="noopener">
			<i class="material-icons">play_arrow</i>
		</a>
	</figure>`

	got, ok := extractIwaraURLFromHTML(html)
	if !ok {
		t.Fatal("extractIwaraURLFromHTML returned false")
	}
	want := "https://www.iwara.tv/video/CNCQNZEfKO8QYI/mmdparty-tonight-jane-doe"
	if got != want {
		t.Fatalf("extractIwaraURLFromHTML = %q, want %q", got, want)
	}
}

func TestResolveOrenoMovieFetchesPageAndExtractsIwaraURL(t *testing.T) {
	const iwaraURL = "https://www.iwara.tv/video/abc123XYZ/fetched-from-oreno"
	seenUserAgent := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUserAgent = r.UserAgent()
		_, _ = w.Write([]byte(`<a href="` + iwaraURL + `" class="video-watch-btn2">watch</a>`))
	}))
	defer server.Close()

	resolver := LinkResolver{
		Client:    server.Client(),
		UserAgent: "test-agent",
	}
	got, err := resolver.resolveOrenoMovie(context.Background(), server.URL+"/movies/123")
	if err != nil {
		t.Fatalf("resolveOrenoMovie returned error: %v", err)
	}
	if got != iwaraURL {
		t.Fatalf("resolveOrenoMovie = %q, want %q", got, iwaraURL)
	}
	if seenUserAgent != "test-agent" {
		t.Fatalf("User-Agent = %q, want test-agent", seenUserAgent)
	}
}

func TestIsOrenoMovieURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "movie page",
			raw:  "https://oreno3d.com/movies/347601",
			want: true,
		},
		{
			name: "www movie page",
			raw:  "https://www.oreno3d.com/movies/347601/",
			want: true,
		},
		{
			name: "non movie path",
			raw:  "https://oreno3d.com/authors/597",
			want: false,
		},
		{
			name: "different host",
			raw:  "https://example.com/movies/347601",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOrenoMovieURL(tt.raw); got != tt.want {
				t.Fatalf("isOrenoMovieURL(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
