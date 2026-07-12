package downloader

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultResolveTimeout = 15 * time.Second
	defaultMaxResolveBody = 2 << 20
	defaultUserAgent      = "Swaratelle/1.0 (+https://github.com/eaglestelle/swaratelle)"
)

var (
	iwaraVideoURLRe = regexp.MustCompile(`https?://(?:www\.)?iwara\.tv/video/[A-Za-z0-9]+(?:/[^\s"'<>]*)?`)
	orenoMoviePath  = regexp.MustCompile(`^/movies/[0-9]+/?$`)
)

type LinkResolver struct {
	Client       *http.Client
	UserAgent    string
	MaxBodyBytes int64
}

func ResolveSourceURL(ctx context.Context, raw string) (string, error) {
	resolver := LinkResolver{
		Client: &http.Client{Timeout: defaultResolveTimeout},
	}
	return resolver.Resolve(ctx, raw)
}

func (r LinkResolver) Resolve(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is empty")
	}
	if _, err := ExtractVideoID(raw); err == nil {
		return raw, nil
	}
	if !isOrenoMovieURL(raw) {
		return raw, nil
	}
	return r.resolveOrenoMovie(ctx, raw)
}

func (r LinkResolver) resolveOrenoMovie(ctx context.Context, raw string) (string, error) {
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: defaultResolveTimeout}
	}
	userAgent := r.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	maxBody := r.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxResolveBody
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", fmt.Errorf("build Oreno3D request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch Oreno3D page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fetch Oreno3D page: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", fmt.Errorf("read Oreno3D page: %w", err)
	}
	iwaraURL, ok := extractIwaraURLFromHTML(string(body))
	if !ok {
		return "", errors.New("Oreno3D page did not contain an Iwara video link")
	}
	return iwaraURL, nil
}

func isOrenoMovieURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "oreno3d.com" && host != "www.oreno3d.com" {
		return false
	}
	return orenoMoviePath.MatchString(u.EscapedPath())
}

func extractIwaraURLFromHTML(source string) (string, bool) {
	source = html.UnescapeString(source)
	m := iwaraVideoURLRe.FindString(source)
	if m == "" {
		return "", false
	}
	return m, true
}
