package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"meshserver/internal/config"
)

// StatusHooks provides readiness and version hooks for the management server.
type StatusHooks struct {
	IsReady         func() bool
	Version         func() string
	ConfigSnapshot  func() any
	BlobRoot        string
	ServeBlobRoutes bool
}

// NewHTTPServer builds the lightweight management HTTP server.
func NewHTTPServer(cfg *config.Config, logger *slog.Logger, hooks StatusHooks) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ready := hooks.IsReady != nil && hooks.IsReady()
		if !ready {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
	})

	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		version := "unknown"
		if hooks.Version != nil {
			version = hooks.Version()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version": version,
		})
	})

	if cfg.EnableDebugConfig && hooks.ConfigSnapshot != nil {
		mux.HandleFunc("/debug/config", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, hooks.ConfigSnapshot())
		})
	}

	if hooks.ServeBlobRoutes && hooks.BlobRoot != "" {
		mux.Handle("/blobs/", http.StripPrefix("/blobs/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
			target := path.Join(hooks.BlobRoot, strings.TrimPrefix(cleanPath, "/"))
			http.ServeFile(w, r, target)
		})))
	}

	handler := loggingMiddleware(logger, mux)

	return &http.Server{
		Addr:         cfg.HTTPListenAddr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		BaseContext: func(net.Listener) context.Context {
			return context.Background()
		},
	}
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
	}
}
