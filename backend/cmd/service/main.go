package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"iwaradl-managed/internal/api"
	"iwaradl-managed/internal/db"
	"iwaradl-managed/internal/downloader"
)

const (
	dbPath     = "/data/dedupe.db"
	dataDir    = "/data"
	mediaDir   = "/media"
	scratchDir = "/scratch"
	binPath    = "/app/iwaradl"
	listenAddr = ":8842"
	webDir     = "/app/web"
)

func main() {
	apiToken := os.Getenv("SWARATELLE_API_TOKEN")
	downloadConcurrency := positiveIntEnv("SWARATELLE_DOWNLOAD_CONCURRENCY", 1)

	for _, dir := range []string{dataDir, mediaDir, scratchDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Fatalf("create dir %s: %v", dir, err)
		}
	}

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	dl := &downloader.Downloader{
		BinaryPath:     binPath,
		ScratchDir:     scratchDir,
		MediaDir:       mediaDir,
		Database:       database,
		MaxConcurrency: downloadConcurrency,
	}

	// Downloads are scheduled on demand: no downloader goroutine exists while
	// the queue is idle, and active iwaradl processes are capped by the env var.
	downloadCtx, cancelDownloads := context.WithCancel(context.Background())
	defer cancelDownloads()
	dl.Start(downloadCtx)
	log.Printf("download concurrency: %d", downloadConcurrency)

	srv := &api.Server{DL: dl, DB: database, Token: apiToken, WebDir: webDir}

	httpSrv := &http.Server{
		Addr:              listenAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", listenAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("stopped")
}

func positiveIntEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		log.Printf("invalid %s=%q; using %d", name, raw, fallback)
		return fallback
	}
	return n
}
