package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultProtocolID = "/meshserver/session/1.0.0"
	defaultVersion    = "dev"
)

// Config holds runtime configuration for the meshserver process.
type Config struct {
	ConfigFile             string
	Version                string
	NodeName               string
	MySQLDSN               string
	MySQLAdminDSN          string
	MySQLServerPubKeyPath  string
	MySQLConnMaxLifetime   time.Duration
	MySQLMaxIdleConns      int
	MySQLMaxOpenConns      int
	MySQLConnectTimeout    time.Duration
	MySQLRetryInterval     time.Duration
	MySQLMaxRetryDuration  time.Duration
	Libp2pListenAddrs      []string
	Libp2pProtocolID       string
	NodeKeyPath            string
	BlobRoot               string
	LogDir                 string
	MigrationsDir          string
	HTTPListenAddr         string
	ReadTimeout            time.Duration
	WriteTimeout           time.Duration
	HeartbeatInterval      time.Duration
	ChallengeTTL           time.Duration
	MaxTextLen             int
	MaxImagesPerMessage    int
	MaxFilesPerMessage     int
	MaxUploadBytes         int64
	DefaultSyncLimit       uint32
	MaxSyncLimit           uint32
	EnableDebugConfig      bool
	ServeBlobsOverHTTP     bool
	BlobURLBase            string
	DHTDiscoveryNamespace  string
	DHTBootstrapPeers      []string
}

type fileConfig struct {
	Version                *string  `json:"version"`
	NodeName               *string  `json:"node_name"`
	MySQLDSN               *string  `json:"mysql_dsn"`
	MySQLAdminDSN          *string  `json:"mysql_admin_dsn"`
	MySQLServerPubKeyPath  *string  `json:"mysql_server_pub_key_path"`
	MySQLConnMaxLifetime   *string  `json:"mysql_conn_max_lifetime"`
	MySQLMaxIdleConns      *int     `json:"mysql_max_idle_conns"`
	MySQLMaxOpenConns      *int     `json:"mysql_max_open_conns"`
	MySQLConnectTimeout    *string  `json:"mysql_connect_timeout"`
	MySQLRetryInterval     *string  `json:"mysql_retry_interval"`
	MySQLMaxRetryDuration  *string  `json:"mysql_max_retry_duration"`
	Libp2pListenAddrs      []string `json:"libp2p_listen_addrs"`
	Libp2pProtocolID       *string  `json:"libp2p_protocol_id"`
	NodeKeyPath            *string  `json:"node_key_path"`
	BlobRoot               *string  `json:"blob_root"`
	LogDir                 *string  `json:"log_dir"`
	MigrationsDir          *string  `json:"migrations_dir"`
	HTTPListenAddr         *string  `json:"http_listen_addr"`
	ReadTimeout            *string  `json:"read_timeout"`
	WriteTimeout           *string  `json:"write_timeout"`
	HeartbeatInterval      *string  `json:"heartbeat_interval"`
	ChallengeTTL           *string  `json:"challenge_ttl"`
	MaxTextLen             *int     `json:"max_text_len"`
	MaxImagesPerMessage    *int     `json:"max_images_per_message"`
	MaxFilesPerMessage     *int     `json:"max_files_per_message"`
	MaxUploadBytes         *int64   `json:"max_upload_bytes"`
	DefaultSyncLimit       *uint32  `json:"default_sync_limit"`
	MaxSyncLimit           *uint32  `json:"max_sync_limit"`
	EnableDebugConfig      *bool    `json:"enable_debug_config"`
	ServeBlobsOverHTTP     *bool    `json:"serve_blobs_over_http"`
	BlobURLBase            *string  `json:"blob_url_base"`
	DHTDiscoveryNamespace  *string  `json:"dht_discovery_namespace"`
	DHTBootstrapPeers      []string `json:"dht_bootstrap_peers"`
}

// Load loads configuration from defaults, optional JSON config file, and env vars.
func Load() (*Config, error) {
	cfg := defaults()
	cfg.ConfigFile = firstNonEmpty(os.Getenv("MESHSERVER_CONFIG_FILE"), filepath.Join("docker-compose", "data", "config", "meshserver.json"))

	if _, err := os.Stat(cfg.ConfigFile); err == nil {
		if err := applyFile(cfg, cfg.ConfigFile); err != nil {
			return nil, err
		}
	}

	applyEnv(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate validates a runtime configuration.
func (c *Config) Validate() error {
	if c.MySQLDSN == "" {
		return errors.New("mysql dsn is required")
	}
	if len(c.Libp2pListenAddrs) == 0 {
		return errors.New("at least one libp2p listen addr is required")
	}
	if c.Libp2pProtocolID == "" {
		return errors.New("libp2p protocol id is required")
	}
	if c.NodeKeyPath == "" {
		return errors.New("node key path is required")
	}
	if c.BlobRoot == "" {
		return errors.New("blob root is required")
	}
	if c.LogDir == "" {
		return errors.New("log dir is required")
	}
	if c.MigrationsDir == "" {
		return errors.New("migrations dir is required")
	}
	if c.HTTPListenAddr == "" {
		return errors.New("http listen addr is required")
	}
	if c.MaxTextLen <= 0 || c.MaxImagesPerMessage <= 0 || c.MaxFilesPerMessage <= 0 || c.MaxUploadBytes <= 0 {
		return errors.New("message and upload limits must be positive")
	}
	if c.DefaultSyncLimit == 0 || c.MaxSyncLimit == 0 || c.DefaultSyncLimit > c.MaxSyncLimit {
		return errors.New("sync limits are invalid")
	}
	return nil
}

func defaults() *Config {
	return &Config{
		Version:                defaultVersion,
		NodeName:               "meshserver-node",
		MySQLDSN:               "meshserver:meshserver@tcp(127.0.0.1:3306)/meshserver?parseTime=true&multiStatements=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
		MySQLAdminDSN:          "",
		MySQLServerPubKeyPath:  "",
		MySQLConnMaxLifetime:   5 * time.Minute,
		MySQLMaxIdleConns:      5,
		MySQLMaxOpenConns:      20,
		MySQLConnectTimeout:    60 * time.Second,
		MySQLRetryInterval:     3 * time.Second,
		MySQLMaxRetryDuration:  2 * time.Minute,
		Libp2pListenAddrs:      []string{"/ip4/0.0.0.0/tcp/4001"},
		Libp2pProtocolID:       defaultProtocolID,
		NodeKeyPath:            filepath.Join("docker-compose", "data", "config", "node.key"),
		BlobRoot:               filepath.Join("docker-compose", "data", "blobs"),
		LogDir:                 filepath.Join("docker-compose", "data", "logs"),
		MigrationsDir:          "migrations",
		HTTPListenAddr:         ":8080",
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           15 * time.Second,
		HeartbeatInterval:      30 * time.Second,
		ChallengeTTL:           5 * time.Minute,
		MaxTextLen:             4000,
		MaxImagesPerMessage:    9,
		MaxFilesPerMessage:     5,
		MaxUploadBytes:         10 << 20,
		DefaultSyncLimit:       50,
		MaxSyncLimit:           200,
		EnableDebugConfig:      false,
		ServeBlobsOverHTTP:     true,
		BlobURLBase:            "/blobs/",
		DHTDiscoveryNamespace:  "meshserver",
		DHTBootstrapPeers:      nil,
	}
}

func applyFile(cfg *Config, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file %q: %w", path, err)
	}

	var fc fileConfig
	if err := json.Unmarshal(raw, &fc); err != nil {
		return fmt.Errorf("parse config file %q: %w", path, err)
	}

	applyString(&cfg.Version, fc.Version)
	applyString(&cfg.NodeName, fc.NodeName)
	applyString(&cfg.MySQLDSN, fc.MySQLDSN)
	applyString(&cfg.MySQLAdminDSN, fc.MySQLAdminDSN)
	applyString(&cfg.MySQLServerPubKeyPath, fc.MySQLServerPubKeyPath)
	applyDuration(&cfg.MySQLConnMaxLifetime, fc.MySQLConnMaxLifetime)
	applyInt(&cfg.MySQLMaxIdleConns, fc.MySQLMaxIdleConns)
	applyInt(&cfg.MySQLMaxOpenConns, fc.MySQLMaxOpenConns)
	applyDuration(&cfg.MySQLConnectTimeout, fc.MySQLConnectTimeout)
	applyDuration(&cfg.MySQLRetryInterval, fc.MySQLRetryInterval)
	applyDuration(&cfg.MySQLMaxRetryDuration, fc.MySQLMaxRetryDuration)
	if len(fc.Libp2pListenAddrs) > 0 {
		cfg.Libp2pListenAddrs = fc.Libp2pListenAddrs
	}
	applyString(&cfg.Libp2pProtocolID, fc.Libp2pProtocolID)
	applyString(&cfg.NodeKeyPath, fc.NodeKeyPath)
	applyString(&cfg.BlobRoot, fc.BlobRoot)
	applyString(&cfg.LogDir, fc.LogDir)
	applyString(&cfg.MigrationsDir, fc.MigrationsDir)
	applyString(&cfg.HTTPListenAddr, fc.HTTPListenAddr)
	applyDuration(&cfg.ReadTimeout, fc.ReadTimeout)
	applyDuration(&cfg.WriteTimeout, fc.WriteTimeout)
	applyDuration(&cfg.HeartbeatInterval, fc.HeartbeatInterval)
	applyDuration(&cfg.ChallengeTTL, fc.ChallengeTTL)
	applyInt(&cfg.MaxTextLen, fc.MaxTextLen)
	applyInt(&cfg.MaxImagesPerMessage, fc.MaxImagesPerMessage)
	applyInt(&cfg.MaxFilesPerMessage, fc.MaxFilesPerMessage)
	applyInt64(&cfg.MaxUploadBytes, fc.MaxUploadBytes)
	applyUint32(&cfg.DefaultSyncLimit, fc.DefaultSyncLimit)
	applyUint32(&cfg.MaxSyncLimit, fc.MaxSyncLimit)
	applyBool(&cfg.EnableDebugConfig, fc.EnableDebugConfig)
	applyBool(&cfg.ServeBlobsOverHTTP, fc.ServeBlobsOverHTTP)
	applyString(&cfg.BlobURLBase, fc.BlobURLBase)
	applyString(&cfg.DHTDiscoveryNamespace, fc.DHTDiscoveryNamespace)
	if len(fc.DHTBootstrapPeers) > 0 {
		cfg.DHTBootstrapPeers = fc.DHTBootstrapPeers
	}
	return nil
}

func applyEnv(cfg *Config) {
	applyEnvString("MESHSERVER_VERSION", &cfg.Version)
	applyEnvString("MESHSERVER_NODE_NAME", &cfg.NodeName)
	applyEnvString("MESHSERVER_MYSQL_DSN", &cfg.MySQLDSN)
	applyEnvString("MESHSERVER_MYSQL_ADMIN_DSN", &cfg.MySQLAdminDSN)
	applyEnvString("MESHSERVER_MYSQL_SERVER_PUB_KEY_PATH", &cfg.MySQLServerPubKeyPath)
	applyEnvDuration("MESHSERVER_MYSQL_CONN_MAX_LIFETIME", &cfg.MySQLConnMaxLifetime)
	applyEnvInt("MESHSERVER_MYSQL_MAX_IDLE_CONNS", &cfg.MySQLMaxIdleConns)
	applyEnvInt("MESHSERVER_MYSQL_MAX_OPEN_CONNS", &cfg.MySQLMaxOpenConns)
	applyEnvDuration("MESHSERVER_MYSQL_CONNECT_TIMEOUT", &cfg.MySQLConnectTimeout)
	applyEnvDuration("MESHSERVER_MYSQL_RETRY_INTERVAL", &cfg.MySQLRetryInterval)
	applyEnvDuration("MESHSERVER_MYSQL_MAX_RETRY_DURATION", &cfg.MySQLMaxRetryDuration)
	if raw := os.Getenv("MESHSERVER_LIBP2P_LISTEN_ADDRS"); raw != "" {
		cfg.Libp2pListenAddrs = splitList(raw)
	}
	applyEnvString("MESHSERVER_LIBP2P_PROTOCOL_ID", &cfg.Libp2pProtocolID)
	applyEnvString("MESHSERVER_NODE_KEY_PATH", &cfg.NodeKeyPath)
	applyEnvString("MESHSERVER_BLOB_ROOT", &cfg.BlobRoot)
	applyEnvString("MESHSERVER_LOG_DIR", &cfg.LogDir)
	applyEnvString("MESHSERVER_MIGRATIONS_DIR", &cfg.MigrationsDir)
	applyEnvString("MESHSERVER_HTTP_LISTEN_ADDR", &cfg.HTTPListenAddr)
	applyEnvDuration("MESHSERVER_READ_TIMEOUT", &cfg.ReadTimeout)
	applyEnvDuration("MESHSERVER_WRITE_TIMEOUT", &cfg.WriteTimeout)
	applyEnvDuration("MESHSERVER_HEARTBEAT_INTERVAL", &cfg.HeartbeatInterval)
	applyEnvDuration("MESHSERVER_CHALLENGE_TTL", &cfg.ChallengeTTL)
	applyEnvInt("MESHSERVER_MAX_TEXT_LEN", &cfg.MaxTextLen)
	applyEnvInt("MESHSERVER_MAX_IMAGES_PER_MESSAGE", &cfg.MaxImagesPerMessage)
	applyEnvInt("MESHSERVER_MAX_FILES_PER_MESSAGE", &cfg.MaxFilesPerMessage)
	applyEnvInt64("MESHSERVER_MAX_UPLOAD_BYTES", &cfg.MaxUploadBytes)
	applyEnvUint32("MESHSERVER_DEFAULT_SYNC_LIMIT", &cfg.DefaultSyncLimit)
	applyEnvUint32("MESHSERVER_MAX_SYNC_LIMIT", &cfg.MaxSyncLimit)
	applyEnvBool("MESHSERVER_ENABLE_DEBUG_CONFIG", &cfg.EnableDebugConfig)
	applyEnvBool("MESHSERVER_SERVE_BLOBS_OVER_HTTP", &cfg.ServeBlobsOverHTTP)
	applyEnvString("MESHSERVER_BLOB_URL_BASE", &cfg.BlobURLBase)
	applyEnvString("MESHSERVER_DHT_DISCOVERY_NAMESPACE", &cfg.DHTDiscoveryNamespace)
	if raw := os.Getenv("MESHSERVER_DHT_BOOTSTRAP_PEERS"); raw != "" {
		cfg.DHTBootstrapPeers = splitList(raw)
	}
}

func applyString(dst *string, src *string) {
	if src != nil && *src != "" {
		*dst = *src
	}
}

func applyDuration(dst *time.Duration, src *string) {
	if src == nil || *src == "" {
		return
	}
	if parsed, err := time.ParseDuration(*src); err == nil {
		*dst = parsed
	}
}

func applyInt(dst *int, src *int) {
	if src != nil {
		*dst = *src
	}
}

func applyInt64(dst *int64, src *int64) {
	if src != nil {
		*dst = *src
	}
}

func applyUint32(dst *uint32, src *uint32) {
	if src != nil {
		*dst = *src
	}
}

func applyBool(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
}

func applyEnvString(key string, dst *string) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		*dst = value
	}
}

func applyEnvDuration(key string, dst *time.Duration) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			*dst = parsed
		}
	}
}

func applyEnvInt(key string, dst *int) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			*dst = parsed
		}
	}
}

func applyEnvInt64(key string, dst *int64) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			*dst = parsed
		}
	}
}

func applyEnvUint32(key string, dst *uint32) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.ParseUint(value, 10, 32); err == nil {
			*dst = uint32(parsed)
		}
	}
}

func applyEnvBool(key string, dst *bool) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			*dst = parsed
		}
	}
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
