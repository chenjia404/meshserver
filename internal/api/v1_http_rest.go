package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
)

func registerV1RESTRoutes(mux *http.ServeMux, logger *slog.Logger, deps V1HTTPDeps) {
	mux.HandleFunc("POST /v1/spaces", func(w http.ResponseWriter, r *http.Request) {
		handleV1CreateSpace(w, r, logger, deps)
	})
	mux.HandleFunc("GET /v1/permissions/create-space", func(w http.ResponseWriter, r *http.Request) {
		handleV1GetCreateSpacePermissions(w, r, logger, deps)
	})
	mux.HandleFunc("GET /v1/spaces/{space_id}/permissions/create-group", func(w http.ResponseWriter, r *http.Request) {
		handleV1GetCreateGroupPermissions(w, r, logger, deps)
	})
	mux.HandleFunc("PATCH /v1/spaces/{space_id}/members/{target_user_id}/role", func(w http.ResponseWriter, r *http.Request) {
		handleV1AdminSetMemberRole(w, r, logger, deps)
	})
	mux.HandleFunc("PUT /v1/spaces/{space_id}/settings/channel_creation", func(w http.ResponseWriter, r *http.Request) {
		handleV1AdminSetChannelCreation(w, r, logger, deps)
	})
	mux.HandleFunc("PUT /v1/channels/{channel_id}/settings/auto_delete", func(w http.ResponseWriter, r *http.Request) {
		handleV1AdminSetGroupAutoDelete(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/spaces/{space_id}/invitations", func(w http.ResponseWriter, r *http.Request) {
		handleV1InviteMember(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/spaces/{space_id}/kick", func(w http.ResponseWriter, r *http.Request) {
		handleV1KickMember(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/spaces/{space_id}/ban", func(w http.ResponseWriter, r *http.Request) {
		handleV1BanMember(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/spaces/{space_id}/unban", func(w http.ResponseWriter, r *http.Request) {
		handleV1UnbanMember(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/spaces/{space_id}/groups", func(w http.ResponseWriter, r *http.Request) {
		handleV1CreateGroup(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/spaces/{space_id}/channels", func(w http.ResponseWriter, r *http.Request) {
		handleV1CreateChannel(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/channels/{channel_id}/delivered_ack", func(w http.ResponseWriter, r *http.Request) {
		handleV1DeliveredAck(w, r, logger, deps)
	})
	mux.HandleFunc("POST /v1/channels/{channel_id}/read", func(w http.ResponseWriter, r *http.Request) {
		handleV1ReadUpdate(w, r, logger, deps)
	})
}

type targetUserIDBody struct {
	TargetUserID string `json:"target_user_id"`
}

type allowChannelCreationBody struct {
	AllowChannelCreation bool `json:"allow_channel_creation"`
}

type autoDeleteBody struct {
	AutoDeleteAfterSeconds uint32 `json:"auto_delete_after_seconds"`
}

type memberRoleBody struct {
	Role sessionv1.MemberRole `json:"role"`
}

type deliverAckBody struct {
	AckedSeq uint64 `json:"acked_seq"`
}

type readUpdateBody struct {
	LastReadSeq uint64 `json:"last_read_seq"`
}

func handleV1CreateSpace(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var req sessionv1.CreateSpaceReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp, err := deps.Session.CreateSpaceForAPI(r.Context(), claims.UserDBID, claims.PeerID, &req)
	if err != nil {
		writeServiceErr(w, logger, "v1 create space", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1GetCreateSpacePermissions(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, ok := requireBearerJWT(w, r, deps.JWTSecret)
	if !ok {
		return
	}
	resp, err := deps.Session.GetCreateSpacePermissionsForAPI(r.Context(), claims.PeerID)
	if err != nil {
		logger.Error("v1 get create space permissions", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1GetCreateGroupPermissions(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	resp, err := deps.Session.GetCreateGroupPermissionsForAPI(r.Context(), claims.UserDBID, claims.PeerID, spaceID)
	if err != nil {
		writeServiceErr(w, logger, "v1 get create group permissions", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1AdminSetMemberRole(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodPatch {
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
	targetUID := strings.TrimSpace(r.PathValue("target_user_id"))
	if targetUID == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid target_user_id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var parsed memberRoleBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp, err := deps.Session.AdminSetSpaceMemberRoleForAPI(r.Context(), claims.UserDBID, spaceID, targetUID, parsed.Role)
	if err != nil {
		writeServiceErr(w, logger, "v1 admin set member role", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1AdminSetChannelCreation(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodPut {
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var parsed allowChannelCreationBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp, err := deps.Session.AdminSetSpaceChannelCreationForAPI(r.Context(), claims.UserDBID, spaceID, parsed.AllowChannelCreation)
	if err != nil {
		writeServiceErr(w, logger, "v1 admin set channel creation", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1AdminSetGroupAutoDelete(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodPut {
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
	var parsed autoDeleteBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	resp, err := deps.Session.AdminSetGroupAutoDeleteForAPI(r.Context(), claims.UserDBID, channelID, parsed.AutoDeleteAfterSeconds)
	if err != nil {
		writeServiceErr(w, logger, "v1 admin set auto delete", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1InviteMember(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var parsed targetUserIDBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(parsed.TargetUserID) == "" {
		writeJSONError(w, http.StatusBadRequest, "target_user_id is required")
		return
	}
	resp, err := deps.Session.InviteSpaceMemberForAPI(r.Context(), claims.UserDBID, claims.PeerID, spaceID, parsed.TargetUserID)
	if err != nil {
		writeServiceErr(w, logger, "v1 invite member", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1KickMember(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var parsed targetUserIDBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(parsed.TargetUserID) == "" {
		writeJSONError(w, http.StatusBadRequest, "target_user_id is required")
		return
	}
	resp, err := deps.Session.KickSpaceMemberForAPI(r.Context(), claims.UserDBID, claims.PeerID, spaceID, parsed.TargetUserID)
	if err != nil {
		writeServiceErr(w, logger, "v1 kick member", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1BanMember(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var parsed targetUserIDBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(parsed.TargetUserID) == "" {
		writeJSONError(w, http.StatusBadRequest, "target_user_id is required")
		return
	}
	resp, err := deps.Session.BanSpaceMemberForAPI(r.Context(), claims.UserDBID, claims.PeerID, spaceID, parsed.TargetUserID)
	if err != nil {
		writeServiceErr(w, logger, "v1 ban member", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1UnbanMember(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var parsed targetUserIDBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(parsed.TargetUserID) == "" {
		writeJSONError(w, http.StatusBadRequest, "target_user_id is required")
		return
	}
	resp, err := deps.Session.UnbanSpaceMemberForAPI(r.Context(), claims.UserDBID, claims.PeerID, spaceID, parsed.TargetUserID)
	if err != nil {
		writeServiceErr(w, logger, "v1 unban member", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1CreateGroup(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var req sessionv1.CreateGroupReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.SpaceId = spaceID
	resp, err := deps.Session.CreateGroupForAPI(r.Context(), claims.UserDBID, claims.PeerID, &req)
	if err != nil {
		writeServiceErr(w, logger, "v1 create group", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1CreateChannel(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxV1JSONBody))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body failed")
		return
	}
	var req sessionv1.CreateChannelReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.SpaceId = spaceID
	resp, err := deps.Session.CreateChannelForAPI(r.Context(), claims.UserDBID, claims.PeerID, &req)
	if err != nil {
		writeServiceErr(w, logger, "v1 create channel", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleV1DeliveredAck(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	var parsed deliverAckBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := deps.Session.AckDeliveredForAPI(r.Context(), claims.UserDBID, channelID, parsed.AckedSeq); err != nil {
		writeServiceErr(w, logger, "v1 delivered ack", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleV1ReadUpdate(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
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
	var parsed readUpdateBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := deps.Session.UpdateReadForAPI(r.Context(), claims.UserDBID, channelID, parsed.LastReadSeq); err != nil {
		writeServiceErr(w, logger, "v1 read update", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
