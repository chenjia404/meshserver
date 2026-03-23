package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"meshserver/internal/ipfsnode"
	"meshserver/internal/ipfspin"

	cid "github.com/ipfs/go-cid"
)

type ipfsJSONError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeIPFSError(w http.ResponseWriter, code, msg string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	var e ipfsJSONError
	e.Error.Code = code
	e.Error.Message = msg
	_ = json.NewEncoder(w).Encode(e)
}

func registerIPFSRoutes(mux *http.ServeMux, logger *slog.Logger, emb *ipfsnode.EmbeddedIPFS) {
	if emb == nil {
		return
	}
	h := &ipfsHTTPHandler{log: logger, emb: emb}
	if emb.GatewayEnabled() {
		mux.HandleFunc("/ipfs", h.serveIPFSGateway)
		mux.HandleFunc("/ipfs/", h.serveIPFSGateway)
	}
	if emb.APIEnabled() {
		mux.HandleFunc("/api/ipfs/add", h.handleIPFSAdd)
		mux.HandleFunc("/api/ipfs/add-dir", h.handleIPFSAddDir)
		mux.HandleFunc("/api/ipfs/stat/", h.handleIPFSStat)
		mux.HandleFunc("/api/ipfs/pin/", h.handleIPFSPin)
	}
}

type ipfsHTTPHandler struct {
	log *slog.Logger
	emb *ipfsnode.EmbeddedIPFS
}

func (h *ipfsHTTPHandler) handleIPFSAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeIPFSError(w, "BAD_REQUEST", "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.emb == nil || !h.emb.APIEnabled() {
		http.NotFound(w, r)
		return
	}
	h.execIPFSAdd(w, r)
}

func (h *ipfsHTTPHandler) handleIPFSGatewayUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeIPFSError(w, "BAD_REQUEST", "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.emb == nil || !h.emb.GatewayWritable() {
		http.NotFound(w, r)
		return
	}
	h.execIPFSAdd(w, r)
}

func (h *ipfsHTTPHandler) execIPFSAdd(w http.ResponseWriter, r *http.Request) {
	maxB := h.emb.MaxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, maxB+1)
	if err := r.ParseMultipartForm(maxB + 1); err != nil {
		var me *http.MaxBytesError
		if errors.As(err, &me) {
			writeIPFSError(w, "PAYLOAD_TOO_LARGE", "upload exceeds limit", http.StatusRequestEntityTooLarge)
			return
		}
		writeIPFSError(w, "BAD_REQUEST", "invalid multipart form", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeIPFSError(w, "BAD_REQUEST", "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	pin := h.emb.AutoPinOnAdd()
	if v := r.FormValue("pin"); v != "" {
		pin, _ = strconv.ParseBool(v)
	}
	rawLeaves := h.emb.RawLeaves()
	if v := r.FormValue("rawLeaves"); v != "" {
		rawLeaves, _ = strconv.ParseBool(v)
	}
	hashFunction := h.emb.HashFunction()
	if v := r.FormValue("hashFunction"); v != "" {
		hashFunction = v
	}
	chunker := h.emb.Chunker()
	if v := r.FormValue("chunker"); v != "" {
		chunker = v
	}
	cidVer := h.emb.CIDVersion()
	if v := r.FormValue("cidVersion"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cidVer = n
		}
	}
	if cidVer != 1 {
		writeIPFSError(w, "BAD_REQUEST", "only cidVersion=1 supported", http.StatusBadRequest)
		return
	}

	opt := ipfsnode.AddFileOptions{
		Filename:     r.FormValue("filename"),
		RawLeaves:    rawLeaves,
		CIDVersion:   cidVer,
		HashFunction: hashFunction,
		Chunker:      chunker,
		Pin:          pin,
	}
	svc := h.emb.Service()
	c, err := svc.AddFile(r.Context(), file, opt)
	if err != nil {
		h.log.Warn("ipfs add failed", "error", err)
		writeIPFSError(w, "INTERNAL_ERROR", "add failed", http.StatusInternalServerError)
		return
	}
	st, err := svc.Stat(r.Context(), c)
	if err != nil {
		h.log.Warn("ipfs add stat failed", "error", err)
	}
	size := int64(0)
	pinned := pin
	if st != nil {
		size = st.Size
		pinned = st.Pinned
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"cid":    c.String(),
		"size":   size,
		"pinned": pinned,
	})
}

func (h *ipfsHTTPHandler) serveIPFSGateway(w http.ResponseWriter, r *http.Request) {
	if h.emb == nil {
		http.NotFound(w, r)
		return
	}
	gw := h.emb.GatewayHandler()
	p := r.URL.Path
	if h.emb.GatewayWritable() && (p == "/ipfs" || p == "/ipfs/") {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			h.handleIPFSGatewayUpload(w, r)
			return
		}
	}
	if strings.HasPrefix(p, "/ipfs/") && p != "/ipfs/" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeIPFSError(w, "BAD_REQUEST", "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}
	gw.ServeHTTP(w, r)
}

func (h *ipfsHTTPHandler) handleIPFSAddDir(w http.ResponseWriter, r *http.Request) {
	if h.emb == nil || !h.emb.APIEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeIPFSError(w, "BAD_REQUEST", "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

func (h *ipfsHTTPHandler) handleIPFSStat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeIPFSError(w, "BAD_REQUEST", "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.emb == nil || !h.emb.APIEnabled() {
		http.NotFound(w, r)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/ipfs/stat/")
	suffix = strings.Trim(suffix, "/")
	if suffix == "" {
		writeIPFSError(w, "BAD_REQUEST", "missing cid", http.StatusBadRequest)
		return
	}
	c, err := cid.Decode(suffix)
	if err != nil {
		writeIPFSError(w, "INVALID_CID", "invalid cid", http.StatusBadRequest)
		return
	}
	st, err := h.emb.Service().Stat(r.Context(), c)
	if err != nil {
		writeIPFSError(w, "CID_NOT_FOUND", "content not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"cid":      st.CID,
		"size":     st.Size,
		"numLinks": st.NumLinks,
		"local":    st.Local,
		"pinned":   st.Pinned,
	})
}

type pinBody struct {
	Recursive bool `json:"recursive"`
}

func (h *ipfsHTTPHandler) handleIPFSPin(w http.ResponseWriter, r *http.Request) {
	if h.emb == nil || !h.emb.APIEnabled() {
		http.NotFound(w, r)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/ipfs/pin/")
	suffix = strings.Trim(suffix, "/")
	if suffix == "" {
		writeIPFSError(w, "BAD_REQUEST", "missing cid", http.StatusBadRequest)
		return
	}
	c, err := cid.Decode(suffix)
	if err != nil {
		writeIPFSError(w, "INVALID_CID", "invalid cid", http.StatusBadRequest)
		return
	}
	svc := h.emb.Service()
	switch r.Method {
	case http.MethodPost:
		recursive := true
		if r.ContentLength > 0 {
			var b pinBody
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&b); err == nil {
				recursive = b.Recursive
			}
		}
		if err := svc.Pin(r.Context(), c, recursive); err != nil {
			if errors.Is(err, ipfspin.ErrNotImplemented) {
				writeIPFSError(w, "NOT_IMPLEMENTED", err.Error(), http.StatusNotImplemented)
				return
			}
			writeIPFSError(w, "PIN_FAILED", err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cid": c.String()})
	case http.MethodDelete:
		recursive := true
		if err := svc.Unpin(r.Context(), c, recursive); err != nil {
			if errors.Is(err, ipfspin.ErrNotImplemented) {
				writeIPFSError(w, "NOT_IMPLEMENTED", err.Error(), http.StatusNotImplemented)
				return
			}
			writeIPFSError(w, "UNPIN_FAILED", err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cid": c.String()})
	default:
		writeIPFSError(w, "BAD_REQUEST", "method not allowed", http.StatusMethodNotAllowed)
	}
}
