package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
	"meshserver/internal/media"
	"meshserver/internal/repository"
	"meshserver/internal/service"
	"meshserver/internal/session"
)

const maxV1JSONBody = 8 << 20 // 8 MiB (inline attachments)

// V1HTTPDeps wires authenticated REST handlers to the same logic as libp2p session.
type V1HTTPDeps struct {
	Session        *session.Manager
	Users          repository.UserRepository
	Media          service.MediaService
	MaxUploadBytes int64
	JWTSecret      []byte
}

func registerV1APIRoutes(mux *http.ServeMux, logger *slog.Logger, deps V1HTTPDeps) {
	if deps.Session == nil || deps.Users == nil || len(deps.JWTSecret) == 0 {
		return
	}
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		handleV1Me(w, r, logger, deps)
	})
	mux.HandleFunc("GET /v1/spaces", func(w http.ResponseWriter, r *http.Request) {
		handleV1ListSpaces(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/spaces/{space_id}/join", func(w http.ResponseWriter, r *http.Request) {
		handleV1JoinSpace(w, r, logger, deps)
	})
	mux.HandleFunc("GET /v1/spaces/{space_id}/members", func(w http.ResponseWriter, r *http.Request) {
		handleV1ListSpaceMembers(w, r, logger, deps)
	})
	mux.HandleFunc("GET /v1/spaces/{space_id}/channels", func(w http.ResponseWriter, r *http.Request) {
		handleV1ListChannels(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/channels/{channel_id}/messages", func(w http.ResponseWriter, r *http.Request) {
		handleV1SendMessage(w, r, logger, deps)
	})
	mux.HandleFunc("GET /v1/channels/{channel_id}/sync", func(w http.ResponseWriter, r *http.Request) {
		handleV1SyncChannel(w, r, logger, deps)
	})
	mux.HandleFunc("GET /v1/media/{media_id}", func(w http.ResponseWriter, r *http.Request) {
		handleV1GetMedia(w, r, logger, deps)
	})
	if deps.Media != nil && deps.MaxUploadBytes > 0 {
		mux.HandleFunc("POST /v1/media", func(w http.ResponseWriter, r *http.Request) {
			handleV1UploadMedia(w, r, logger, deps)
		})
	}
	registerV1RESTRoutes(mux, logger, deps)
	registerV1WebSocketRoute(mux, logger, deps)
}

func requireBearerJWT(w http.ResponseWriter, r *http.Request, secret []byte) (*AccessTokenClaims, bool) {
	claims, err := ClaimsFromHTTPRequest(r, secret)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	return claims, true
}

func handleV1Me(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	u, err := deps.Users.GetByID(r.Context(), claims.UserDBID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		logger.Error("v1 me failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "load user failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_db_id":    u.ID,
		"user_id":       u.UserID,
		"peer_id":       u.PeerID,
		"display_name":  u.DisplayName,
		"avatar_url":    u.AvatarURL,
		"bio":           u.Bio,
		"last_login_at": u.LastLoginAt.UTC().Format(time.RFC3339Nano),
	})
}

func handleV1ListSpaces(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	spaces, err := deps.Session.ListSpacesForAPI(r.Context(), claims.UserDBID, claims.PeerID)
	if err != nil {
		logger.Error("v1 list spaces failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "list spaces failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": spaces})
}

func handleV1ListChannels(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	spaceID, err := parseUint32Path(r.PathValue("space_id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid space_id")
		return
	}
	channels, err := deps.Session.ListChannelsForAPI(r.Context(), claims.UserDBID, spaceID)
	if err != nil {
		logger.Error("v1 list channels failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "list channels failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"space_id": spaceID,
		"channels": channels,
	})
}

func handleV1JoinSpace(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	spaceID, err := parseUint32Path(r.PathValue("space_id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid space_id")
		return
	}
	sum, err := deps.Session.JoinSpaceForAPI(r.Context(), claims.UserDBID, claims.PeerID, spaceID)
	if err != nil {
		writeServiceErr(w, logger, "v1 join space", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"space_id": spaceID,
		"space":    sum,
		"message":  "joined",
	})
}

func handleV1ListSpaceMembers(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	spaceID, err := parseUint32Path(r.PathValue("space_id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid space_id")
		return
	}
	q := r.URL.Query()
	afterID, _ := strconv.ParseUint(q.Get("after_member_id"), 10, 64)
	limitU64, err := strconv.ParseUint(q.Get("limit"), 10, 32)
	if err != nil {
		limitU64 = 0
	}
	limit := uint32(limitU64)
	resp, err := deps.Session.ListSpaceMembersForAPI(r.Context(), claims.UserDBID, spaceID, afterID, limit)
	if err != nil {
		writeServiceErr(w, logger, "v1 list space members", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1GetMedia(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	mediaID := strings.TrimSpace(r.PathValue("media_id"))
	if mediaID == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid media_id")
		return
	}
	resp, err := deps.Session.GetMediaForAPI(r.Context(), claims.UserDBID, mediaID)
	if err != nil {
		writeServiceErr(w, logger, "v1 get media", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1UploadMedia(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	if deps.Media == nil {
		writeJSONError(w, http.StatusNotImplemented, "media upload not configured")
		return
	}
	max := deps.MaxUploadBytes
	if max <= 0 {
		max = 10 << 20
	}
	if err := r.ParseMultipartForm(max + 65536); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read file failed")
		return
	}
	if int64(len(content)) > max {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "file exceeds max upload size")
		return
	}
	kind := media.KindFile
	switch strings.TrimSpace(r.FormValue("kind")) {
	case "image":
		kind = media.KindImage
	case "file", "":
		kind = media.KindFile
	default:
		writeJSONError(w, http.StatusBadRequest, "kind must be image or file")
		return
	}
	orig := strings.TrimSpace(r.FormValue("original_name"))
	if orig == "" && hdr != nil {
		orig = filepath.Base(hdr.Filename)
	}
	mimeType := strings.TrimSpace(r.FormValue("mime_type"))
	if mimeType == "" && hdr != nil {
		mimeType = hdr.Header.Get("Content-Type")
	}
	obj, err := deps.Media.SaveUploadedBlob(r.Context(), media.SaveUploadedBlobInput{
		Kind:         kind,
		OriginalName: orig,
		MIMEType:     mimeType,
		Content:      content,
		CreatedBy:    claims.UserDBID,
	})
	if err != nil {
		logger.Warn("v1 upload media", "error", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := map[string]any{
		"ok":            true,
		"media_id":      obj.MediaID,
		"blob_id":       obj.BlobID,
		"mime_type":     obj.MIMEType,
		"size":          obj.Size,
		"original_name": obj.OriginalName,
		"kind":          string(kind),
		"message":       "stored",
	}
	if kind == media.KindFile {
		out["file_cid"] = obj.FileCID
	} else {
		out["sha256"] = obj.SHA256
	}
	writeJSON(w, http.StatusOK, out)
}

type sendMessageHTTPBody struct {
	ClientMsgID string                  `json:"client_msg_id"`
	MessageType sessionv1.MessageType   `json:"message_type"`
	Content     *sessionv1.MessageContent `json:"content"`
}

func handleV1SendMessage(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	channelID, err := parseUint32Path(r.PathValue("channel_id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid channel_id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var parsed sendMessageHTTPBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req := &sessionv1.SendMessageReq{
		ChannelId:   channelID,
		ClientMsgId: parsed.ClientMsgID,
		MessageType: parsed.MessageType,
		Content:     parsed.Content,
	}
	msg, err := deps.Session.SendMessageForAPI(r.Context(), claims.UserDBID, req)
	if err != nil {
		writeServiceErr(w, logger, "v1 send message", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"channel_id":     msg.ChannelDBID,
		"client_msg_id":  msg.ClientMsgID,
		"message_id":     msg.MessageID,
		"seq":            msg.Seq,
		"server_time_ms": uint64(time.Now().UTC().UnixMilli()),
		"message":        "stored",
	})
}

func handleV1SyncChannel(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	channelID, err := parseUint32Path(r.PathValue("channel_id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid channel_id")
		return
	}
	q := r.URL.Query()
	afterSeq, _ := strconv.ParseUint(q.Get("after_seq"), 10, 64)
	limitU64, err := strconv.ParseUint(q.Get("limit"), 10, 32)
	if err != nil || limitU64 == 0 {
		limitU64 = 0 // SyncChannel applies default from config
	}
	limit := uint32(limitU64)
	msgs, nextAfter, hasMore, err := deps.Session.SyncChannelForAPI(r.Context(), claims.UserDBID, channelID, afterSeq, limit)
	if err != nil {
		writeServiceErr(w, logger, "v1 sync channel", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel_id":       channelID,
		"messages":         msgs,
		"next_after_seq":   nextAfter,
		"has_more":         hasMore,
	})
}

func parseUint32Path(s string) (uint32, error) {
	if strings.TrimSpace(s) == "" {
		return 0, strconv.ErrSyntax
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

func writeServiceErr(w http.ResponseWriter, logger *slog.Logger, op string, err error) {
	switch {
	case errors.Is(err, service.ErrForbidden):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, session.ErrAdminRoleRequired):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, session.ErrOwnerRoleRequired):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, session.ErrCreateSpacePermission):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, session.ErrAutoDeleteGroupOnly):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrInvalidMessage):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repository.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	default:
		logger.Warn(op, "error", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
	}
}
