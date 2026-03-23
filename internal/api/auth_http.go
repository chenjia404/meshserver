package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"meshserver/internal/auth"
	"meshserver/internal/config"
)

const maxAuthJSONBody = 1 << 20 // 1 MiB

// AuthHTTPDeps wires libp2p-compatible challenge auth into HTTP + JWT access tokens.
type AuthHTTPDeps struct {
	Service    *auth.Service
	NodePeerID func() string
	JWTSecret  []byte
	AccessTTL  time.Duration
}

func registerV1AuthRoutes(mux *http.ServeMux, logger *slog.Logger, cfg *config.Config, deps AuthHTTPDeps) {
	if deps.Service == nil || len(deps.JWTSecret) == 0 || deps.NodePeerID == nil {
		return
	}
	mux.HandleFunc("/v1/auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		handleAuthChallenge(w, r, logger, cfg, deps)
	})
	mux.HandleFunc("/v1/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		handleAuthVerify(w, r, logger, deps)
	})
}

type authChallengeReq struct {
	ClientPeerID string `json:"client_peer_id"`
}

type authChallengeResp struct {
	ProtocolID  string `json:"protocol_id"`
	NodePeerID  string `json:"node_peer_id"`
	Nonce       string `json:"nonce"`
	IssuedAtMs  uint64 `json:"issued_at_ms"`
	ExpiresAtMs uint64 `json:"expires_at_ms"`
}

type authVerifyReq struct {
	ClientPeerID string `json:"client_peer_id"`
	NodePeerID   string `json:"node_peer_id"`
	Nonce        string `json:"nonce"`
	IssuedAtMs   uint64 `json:"issued_at_ms"`
	ExpiresAtMs  uint64 `json:"expires_at_ms"`
	Signature    string `json:"signature"`
	PublicKey    string `json:"public_key"`
}

type authVerifyResp struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	ExpiresIn   int64        `json:"expires_in"`
	ExpiresAt   string       `json:"expires_at"`
	User        authUserView `json:"user"`
}

type authUserView struct {
	UserDBID    uint64 `json:"user_db_id"`
	UserID      string `json:"user_id"`
	PeerID      string `json:"peer_id"`
	DisplayName string `json:"display_name"`
}

func handleAuthChallenge(w http.ResponseWriter, r *http.Request, logger *slog.Logger, cfg *config.Config, deps AuthHTTPDeps) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAuthJSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var req authChallengeReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	clientPeerID := strings.TrimSpace(req.ClientPeerID)
	if clientPeerID == "" {
		writeJSONError(w, http.StatusBadRequest, "client_peer_id is required")
		return
	}
	nodePeerID := deps.NodePeerID()
	ch, err := deps.Service.IssueChallenge(r.Context(), clientPeerID, nodePeerID)
	if err != nil {
		logger.Warn("http auth challenge failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "issue challenge failed")
		return
	}
	writeJSON(w, http.StatusOK, authChallengeResp{
		ProtocolID:  cfg.Libp2pProtocolID,
		NodePeerID:  ch.NodePeerID,
		Nonce:       base64.StdEncoding.EncodeToString(ch.Nonce),
		IssuedAtMs:  uint64(ch.IssuedAt.UnixMilli()),
		ExpiresAtMs: uint64(ch.ExpiresAt.UnixMilli()),
	})
}

func handleAuthVerify(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps AuthHTTPDeps) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAuthJSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var req authVerifyReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	nonce, err := base64.StdEncoding.DecodeString(req.Nonce)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "nonce must be standard base64")
		return
	}
	sig, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "signature must be standard base64")
		return
	}
	pub, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "public_key must be standard base64")
		return
	}
	issuedAt := time.UnixMilli(int64(req.IssuedAtMs)).UTC()
	expiresAt := time.UnixMilli(int64(req.ExpiresAtMs)).UTC()
	in := auth.VerifyChallengeInput{
		ClientPeerID: strings.TrimSpace(req.ClientPeerID),
		NodePeerID:   strings.TrimSpace(req.NodePeerID),
		Nonce:        nonce,
		IssuedAt:     issuedAt,
		ExpiresAt:    expiresAt,
		Signature:    sig,
		PublicKey:    pub,
	}
	if in.ClientPeerID == "" || in.NodePeerID == "" {
		writeJSONError(w, http.StatusBadRequest, "client_peer_id and node_peer_id are required")
		return
	}
	expectedNode := deps.NodePeerID()
	if in.NodePeerID != expectedNode {
		writeJSONError(w, http.StatusBadRequest, "node_peer_id does not match this server")
		return
	}
	result, err := deps.Service.VerifyChallenge(r.Context(), in)
	if err != nil {
		logger.Info("http auth verify failed", "error", err)
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	ttl := deps.AccessTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	token, exp, err := SignHTTPAccessToken(deps.JWTSecret, result.User, ttl)
	if err != nil {
		logger.Error("sign access token failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	writeJSON(w, http.StatusOK, authVerifyResp{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(time.Until(exp).Seconds()),
		ExpiresAt:   exp.UTC().Format(time.RFC3339Nano),
		User: authUserView{
			UserDBID:    result.User.ID,
			UserID:      result.User.UserID,
			PeerID:      result.User.PeerID,
			DisplayName: result.User.DisplayName,
		},
	})
}
