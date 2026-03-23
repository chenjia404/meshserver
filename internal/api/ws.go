package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
	"meshserver/internal/session"
)

const (
	wsSendBuffer   = 128
	wsMaxMsgSize   = 1 << 16
	wsReadWait     = 90 * time.Second
	wsWriteWait    = 15 * time.Second
	wsPingInterval = 45 * time.Second
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type wsDelivery struct {
	ChannelID uint32
	Event     *sessionv1.MessageEvent
}

type wsClient struct {
	mgr     *session.Manager
	conn    *websocket.Conn
	send    chan wsDelivery
	logger  *slog.Logger
	writeMu sync.Mutex
	subsMu  sync.Mutex
	subs    map[uint32]struct{}
}

func (c *wsClient) RealtimeDeliver(channelID uint32, event *sessionv1.MessageEvent) {
	c.subsMu.Lock()
	_, ok := c.subs[channelID]
	c.subsMu.Unlock()
	if !ok {
		return
	}
	select {
	case c.send <- wsDelivery{ChannelID: channelID, Event: event}:
	default:
		c.logger.Warn("websocket send buffer full, dropping event", "channel_id", channelID)
	}
}

type wsClientAction struct {
	Action    string `json:"action"`
	ChannelID uint32 `json:"channel_id"`
}

type wsServerMsg struct {
	Type string `json:"type"`
	// message_event
	ChannelID uint32                  `json:"channel_id,omitempty"`
	Event     *sessionv1.MessageEvent `json:"event,omitempty"`
	// pong / ack
	Message string `json:"message,omitempty"`
}

func registerV1WebSocketRoute(mux *http.ServeMux, logger *slog.Logger, deps V1HTTPDeps) {
	if deps.Session == nil || len(deps.JWTSecret) == 0 {
		return
	}
	mux.HandleFunc("GET /v1/ws", func(w http.ResponseWriter, r *http.Request) {
		handleV1WebSocket(w, r, logger, deps)
	})
}

func handleV1WebSocket(w http.ResponseWriter, r *http.Request, logger *slog.Logger, deps V1HTTPDeps) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims, err := claimsForWebSocket(r, deps.JWTSecret)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("websocket upgrade failed", "error", err)
		return
	}

	c := &wsClient{
		mgr:    deps.Session,
		conn:   conn,
		send:   make(chan wsDelivery, wsSendBuffer),
		logger: logger,
		subs:   make(map[uint32]struct{}),
	}

	go c.writePump()
	c.readPump(r.Context(), claims.UserDBID, deps)
}

func claimsForWebSocket(r *http.Request, secret []byte) (*AccessTokenClaims, error) {
	if claims, err := ClaimsFromHTTPRequest(r, secret); err == nil {
		return claims, nil
	}
	q := strings.TrimSpace(r.URL.Query().Get("access_token"))
	if q == "" {
		return nil, errors.New("missing token")
	}
	return ParseHTTPAccessToken(secret, q)
}

func (c *wsClient) readPump(ctx context.Context, userID uint64, deps V1HTTPDeps) {
	defer func() {
		c.mgr.UnregisterRealtimeSubscriberAll(c)
		close(c.send)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(wsMaxMsgSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(wsReadWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(wsReadWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) && !errors.Is(err, io.EOF) {
				c.logger.Debug("websocket read ended", "error", err)
			}
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(wsReadWait))

		var msg wsClientAction
		if err := json.Unmarshal(data, &msg); err != nil {
			c.writeJSONError("invalid json")
			continue
		}
		switch strings.ToLower(strings.TrimSpace(msg.Action)) {
		case "ping":
			c.writeServer(wsServerMsg{Type: "pong", Message: "ok"})
		case "subscribe":
			if msg.ChannelID == 0 {
				c.writeJSONError("channel_id required")
				continue
			}
			ok, err := deps.Session.IsChannelMemberForWS(ctx, userID, msg.ChannelID)
			if err != nil || !ok {
				c.writeJSONError("not a channel member")
				continue
			}
			c.subsMu.Lock()
			if _, exists := c.subs[msg.ChannelID]; !exists {
				c.subs[msg.ChannelID] = struct{}{}
				c.mgr.RegisterRealtimeSubscriber(msg.ChannelID, c)
			}
			c.subsMu.Unlock()
			c.writeServer(wsServerMsg{Type: "subscribed", ChannelID: msg.ChannelID, Message: "ok"})
		case "unsubscribe":
			if msg.ChannelID == 0 {
				c.writeJSONError("channel_id required")
				continue
			}
			c.subsMu.Lock()
			delete(c.subs, msg.ChannelID)
			c.subsMu.Unlock()
			c.mgr.UnregisterRealtimeSubscriber(msg.ChannelID, c)
			c.writeServer(wsServerMsg{Type: "unsubscribed", ChannelID: msg.ChannelID, Message: "ok"})
		default:
			c.writeJSONError("unknown action")
		}
	}
}

func (c *wsClient) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return c.conn.WriteJSON(v)
}

func (c *wsClient) writeJSONError(msg string) {
	_ = c.writeJSON(map[string]string{"type": "error", "error": msg})
}

func (c *wsClient) writeServer(m wsServerMsg) {
	_ = c.writeJSON(m)
}

func (c *wsClient) writePump() {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case d, ok := <-c.send:
			if !ok {
				return
			}
			out := wsServerMsg{
				Type:      "message_event",
				ChannelID: d.ChannelID,
				Event:     d.Event,
			}
			if err := c.writeJSON(out); err != nil {
				return
			}
		case <-ticker.C:
			c.writeMu.Lock()
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
