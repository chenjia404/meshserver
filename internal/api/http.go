package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"meshserver/internal/config"
	"meshserver/internal/ipfsnode"
)

// StatusHooks provides readiness and version hooks for the management server.
type StatusHooks struct {
	IsReady         func() bool
	Version         func() string
	ConfigSnapshot  func() any
	BlobRoot        string
	ServeBlobRoutes bool
	// EmbeddedIPFS 可選；啟用時註冊 /ipfs/ 與 /api/ipfs/*（見 ipfs.md）。
	EmbeddedIPFS *ipfsnode.EmbeddedIPFS
}

// NewHTTPServer builds the lightweight management HTTP server.
func NewHTTPServer(cfg *config.Config, logger *slog.Logger, hooks StatusHooks, authDeps AuthHTTPDeps, v1Deps V1HTTPDeps) *http.Server {
	mux := http.NewServeMux()
	registerV1AuthRoutes(mux, logger, cfg, authDeps)
	registerV1APIRoutes(mux, logger, v1Deps)
	registerIPFSRoutes(mux, logger, hooks.EmbeddedIPFS)

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
		blobRoot := hooks.BlobRoot
		mux.Handle("/blobs/", http.StripPrefix("/blobs/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target, err := resolveBlobFilePath(blobRoot, r.URL.Path)
			switch {
			case errors.Is(err, errBlobPathEmpty):
				http.NotFound(w, r)
			case errors.Is(err, errBlobOutsideRoot):
				http.Error(w, "forbidden", http.StatusForbidden)
			case err != nil:
				logger.Error("blob path resolve failed", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			default:
				http.ServeFile(w, r, target)
			}
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

var (
	errBlobPathEmpty   = errors.New("blob path empty")
	errBlobOutsideRoot = errors.New("path outside blob root")
)

// resolveBlobFilePath maps a URL path (after /blobs/ strip) to an absolute file path under blobRoot.
func resolveBlobFilePath(blobRoot, urlPath string) (string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(blobRoot))
	if err != nil {
		return "", fmt.Errorf("blob root: %w", err)
	}
	p := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	rel := strings.TrimPrefix(p, "/")
	if rel == "" || rel == "." {
		return "", errBlobPathEmpty
	}
	joined := filepath.Join(rootAbs, filepath.FromSlash(rel))
	targetAbs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	relFromRoot, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", errBlobOutsideRoot
	}
	if relFromRoot == ".." || strings.HasPrefix(relFromRoot, ".."+string(filepath.Separator)) {
		return "", errBlobOutsideRoot
	}
	return targetAbs, nil
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
	}
}

func writeJSONError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}
