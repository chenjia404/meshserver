package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"

	"meshserver/internal/api"
	"meshserver/internal/auth"
	"meshserver/internal/config"
	"meshserver/internal/db"
	"meshserver/internal/ipfsnode"
	meshlibp2p "meshserver/internal/libp2p"
	"meshserver/internal/logx"
	"meshserver/internal/media"
	"meshserver/internal/repository"
	mysqlrepo "meshserver/internal/repository/mysql"
	"meshserver/internal/service"
	"meshserver/internal/session"
	"meshserver/internal/storage"
)

// App wires together the process lifecycle and dependencies.
type App struct {
	cfg        *config.Config
	logger     *slog.Logger
	logCloser  interface{ Close() error }
	httpServer *http.Server

	db      *sqlx.DB
	node    *meshlibp2p.Node
	ipfsEmb *ipfsnode.EmbeddedIPFS
	session *session.Manager

	readyMu sync.RWMutex
	ready   bool
}

// New creates an application shell.
func New(cfg *config.Config) (*App, error) {
	logger, closer, err := logx.New(cfg)
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg:       cfg,
		logger:    logger,
		logCloser: closer,
	}

	return app, nil
}

// Start starts all runtime dependencies.
func (a *App) Start(ctx context.Context) error {
	if err := a.ensureDirs(); err != nil {
		return err
	}

	conn, err := db.Open(ctx, a.cfg, a.logger)
	if err != nil {
		return err
	}
	a.db = conn

	if err := db.RunMigrations(ctx, conn, a.cfg.MigrationsDir, a.logger); err != nil {
		return err
	}

	store := mysqlrepo.NewStore(conn)

	node, err := meshlibp2p.NewNode(ctx, a.cfg, a.logger, nil)
	if err != nil {
		return err
	}
	a.node = node

	if a.cfg.IPFS.Enabled {
		emb, err := ipfsnode.NewEmbeddedIPFS(ctx, node.Host(), node.Routing(), a.cfg.IPFSBaseDir(), a.cfg.IPFS)
		if err != nil {
			return fmt.Errorf("init embedded ipfs: %w", err)
		}
		a.ipfsEmb = emb
	}

	nodeRecord, err := store.Upsert(ctx, repository.NodeRecord{
		PeerID:      node.PeerID(),
		Name:        a.cfg.NodeName,
		PublicAddrs: node.PublicAddrs(),
		Status:      1,
	})
	if err != nil {
		return err
	}

	if err := store.BootstrapDefaultAdmin(ctx, a.logger, a.cfg.DefaultAdminPeerID, a.cfg.DefaultSpaceID); err != nil {
		return err
	}

	localBlobStore := storage.NewLocalBlobStore(a.cfg.BlobRoot)
	blobService := media.NewBlobService(store, store, localBlobStore, a.cfg.MaxUploadBytes, a.cfg.IPFS)
	mediaService := service.NewMediaService(blobService, store)
	directoryService := service.NewDirectoryService(store, store)
	messagingService := service.NewMessagingService(a.cfg, store, store, store, mediaService)
	authService := auth.NewService(a.cfg, store, store, a.logger)

	jwtSecret, err := api.ResolveHTTPJWTSecret(a.cfg)
	if err != nil {
		return err
	}

	a.session = session.NewManager(a.logger, authService, store, store, directoryService, messagingService, mediaService, store, store, node.PeerID, nodeRecord.ID, a.cfg.BlobURLBase, a.cfg.DefaultAdminPeerID)

	a.httpServer = api.NewHTTPServer(a.cfg, a.logger, api.StatusHooks{
		IsReady:         a.IsReady,
		Version:         func() string { return a.cfg.Version },
		ConfigSnapshot:  func() any { return a.cfg },
		BlobRoot:        a.cfg.BlobRoot,
		ServeBlobRoutes: a.cfg.ServeBlobsOverHTTP,
		EmbeddedIPFS:    a.ipfsEmb,
	}, api.AuthHTTPDeps{
		Service: authService,
		NodePeerID: func() string {
			return node.PeerID()
		},
		JWTSecret: jwtSecret,
		AccessTTL: a.cfg.HTTPAccessTokenTTL,
	}, api.V1HTTPDeps{
		Session:        a.session,
		Users:          store,
		Media:          mediaService,
		MaxUploadBytes: a.cfg.MaxUploadBytes,
		JWTSecret:      jwtSecret,
	})

	node.SetSessionHandler(a.session.HandleStream)
	if err := node.Start(ctx); err != nil {
		return err
	}

	cleanupCtx, cleanupCancel := context.WithCancel(ctx)
	go func() {
		defer cleanupCancel()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				for {
					deleted, err := messagingService.CleanupExpiredMessages(cleanupCtx)
					if err != nil {
						a.logger.Warn("cleanup expired messages failed", "error", err)
						break
					}
					if deleted == 0 {
						break
					}
					a.logger.Info("cleanup expired messages", "deleted", deleted)
				}
			case <-cleanupCtx.Done():
				return
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-time.After(200 * time.Millisecond):
		a.setReady(true)
		a.logger.Info("meshserver started", "peer_id", node.PeerID(), "http_addr", a.cfg.HTTPListenAddr)
		return nil
	case err := <-errCh:
		return fmt.Errorf("start http server: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown gracefully stops the application runtime.
func (a *App) Shutdown(ctx context.Context) error {
	a.setReady(false)

	var errs []error
	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown http server: %w", err))
		}
	}
	if a.ipfsEmb != nil {
		if err := a.ipfsEmb.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close embedded ipfs: %w", err))
		}
	}
	if a.node != nil {
		if err := a.node.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close libp2p node: %w", err))
		}
	}
	if a.db != nil {
		if err := a.db.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			errs = append(errs, fmt.Errorf("close database: %w", err))
		}
	}
	if a.logCloser != nil {
		if err := a.logCloser.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close logger: %w", err))
		}
	}
	return errors.Join(errs...)
}

// IsReady returns current readiness state.
func (a *App) IsReady() bool {
	a.readyMu.RLock()
	defer a.readyMu.RUnlock()
	return a.ready
}

func (a *App) setReady(ready bool) {
	a.readyMu.Lock()
	defer a.readyMu.Unlock()
	a.ready = ready
}

func (a *App) ensureDirs() error {
	dirs := []string{
		a.cfg.BlobRoot,
		a.cfg.LogDir,
		filepath.Dir(a.cfg.NodeKeyPath),
	}
	if a.cfg.IPFS.Enabled {
		dirs = append(dirs, a.cfg.IPFSBaseDir())
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %q: %w", dir, err)
		}
	}
	return nil
}
