# meshserver HTTP API

本文件描述管理用 HTTP 服務的 JSON API。監聽位址由設定 `http_listen_addr` / 環境變數 `MESHSERVER_HTTP_LISTEN_ADDR` 決定（預設見 `internal/config`）。

除非另有說明，請求與回應皆為 **`Content-Type: application/json`**，UTF-8 編碼。

## 錯誤格式

多數失敗回應為 JSON 物件，含單一欄位：

| 欄位    | 型別   | 說明           |
|---------|--------|----------------|
| `error` | string | 人類可讀錯誤訊息 |

HTTP 狀態碼與 `error` 內文會依端點與錯因變化（見各節）。

---

## 與 libp2p 認證對齊

HTTP 的 challenge / verify 流程與 `internal/auth` 中 libp2p 使用的 **`IssueChallenge` / `VerifyChallenge`** 相同：同一組 nonce 儲存與簽名驗證邏輯。

客戶端必須對下列 **UTF-8 位元組序列**（與 `BuildChallengePayload` 一致）做簽名，演算法為 libp2p 身份密鑰對應之簽名（與 stream 上 `AUTH_PROVE` 相同）：

```
protocol_id=<Libp2pProtocolID>
client_peer_id=<客戶端 Peer ID 字串>
node_peer_id=<伺服器 Peer ID 字串>
nonce=<nonce 的標準 Base64，無換行>
issued_at_ms=<毫秒時間戳，整數>
expires_at_ms=<毫秒時間戳，整數>

```

- `protocol_id`：challenge 回應中的 `protocol_id`（與設定 `libp2p_protocol_id` 一致，預設 `/meshserver/session/1.0.0`）。
- `nonce`：challenge 回傳的 `nonce` 字串解碼後的 32 bytes；簽名用的 payload 裡的 `nonce=` 值必須是 **標準 Base64 編碼的同一組 bytes**（與 libp2p 路徑相同）。
- `issued_at_ms` / `expires_at_ms`：須與 challenge / verify 請求中帶上的數值一致。

二進位欄位在 JSON 中的約定：

| 欄位        | 編碼              |
|-------------|-------------------|
| `nonce`     | 標準 Base64（RFC 4648） |
| `signature` | 標準 Base64       |
| `public_key` | libp2p `crypto.MarshalPublicKey` 產生的 bytes 再標準 Base64 |

---

## JWT 存取權杖（verify 成功後）

`POST /v1/auth/verify` 成功時會回傳 `access_token`，為 **HS256** JWT。後續若其他 HTTP API 需要帶入，使用標頭：

```http
Authorization: Bearer <access_token>
```

### Claims（payload）

| Claim   | JSON 鍵名   | 型別   | 說明 |
|---------|-------------|--------|------|
| Subject | `sub`       | string | 等於使用者的對外 `user_id` |
| Issued At | `iat`     | number | 發行時間（Unix 秒） |
| Not Before | `nbf`    | number | 生效時間（Unix 秒） |
| Expiration | `exp`   | number | 過期時間（Unix 秒） |
| （自訂） | `uid`      | number | 資料庫使用者主鍵 |
| （自訂） | `user_id`  | string | 對外使用者 ID |
| （自訂） | `peer_id`  | string | libp2p Peer ID |

### 簽署金鑰

- 若設定 **`http_jwt_secret` / `MESHSERVER_HTTP_JWT_SECRET`** 非空，則以該字串的 UTF-8 bytes 作為 HMAC 金鑰。
- 否則以 **node 金鑰檔**（`node_key_path`）完整檔案內容的 **SHA-256** 作為 HMAC 金鑰（32 bytes）。

### 有效期

由 **`http_access_token_ttl` / `MESHSERVER_HTTP_ACCESS_TOKEN_TTL`** 控制（Go `time.ParseDuration` 格式，例如 `24h`）。未設定時實作預設為 24 小時。

---

## `POST /v1/auth/challenge`

向伺服器索取一次性 challenge（與 libp2p 流程對齊）。

**路由條件**：僅在程序已初始化 `auth.Service`、可解析 JWT 金鑰、且能取得本節點 Peer ID 時註冊；否則此路徑不存在（會由 mux 回 404）。

### 請求 body

| 欄位              | 型別   | 必填 | 說明 |
|-------------------|--------|------|------|
| `client_peer_id`  | string | 是   | 客戶端 libp2p Peer ID（與後續 verify 及簽名公鑰導出 ID 一致） |

### 成功 `200` body

| 欄位            | 型別   | 說明 |
|-----------------|--------|------|
| `protocol_id`   | string | 簽名 payload 用的協議 ID |
| `node_peer_id`  | string | 本 meshserver 節點 Peer ID |
| `nonce`         | string | 32-byte challenge nonce，標準 Base64 |
| `issued_at_ms`  | number | challenge 發行時間（Unix 毫秒，UTC） |
| `expires_at_ms` | number | challenge 過期時間（Unix 毫秒，UTC） |

### 常見錯誤

| HTTP | `error` 範例 |
|------|----------------|
| `405` | `method not allowed`（非 POST） |
| `400` | `invalid json`、`client_peer_id is required`、`read body failed` |
| `500` | `issue challenge failed` |

---

## `POST /v1/auth/verify`

提交客戶端對 challenge payload 的簽名；驗證通過後簽發 JWT 並回傳使用者摘要。

**額外檢查**：請求中的 `node_peer_id` 必須與**當前 HTTP 服務所屬節點**的 Peer ID 一致，否則 `400`。

### 請求 body

| 欄位              | 型別   | 必填 | 說明 |
|-------------------|--------|------|------|
| `client_peer_id`  | string | 是   | 與 challenge 階段相同 |
| `node_peer_id`    | string | 是   | 必須等於伺服器 Peer ID |
| `nonce`           | string | 是   | 與 challenge 回傳相同（標準 Base64） |
| `issued_at_ms`    | number | 是   | 與 challenge 回傳相同 |
| `expires_at_ms`   | number | 是   | 與 challenge 回傳相同 |
| `signature`       | string | 是   | 對 payload 位元組的簽名，標準 Base64 |
| `public_key`      | string | 是   | libp2p marshaled 公鑰，標準 Base64 |

### 成功 `200` body

| 欄位            | 型別   | 說明 |
|-----------------|--------|------|
| `access_token`  | string | JWT 字串 |
| `token_type`    | string | 固定 `Bearer` |
| `expires_in`    | number | 權杖剩餘有效秒數（約值，以伺服器回應當下計算） |
| `expires_at`    | string | 權杖過期時間，RFC3339Nano，UTC |
| `user`          | object | 使用者摘要（見下表） |

#### `user` 物件

| 欄位           | 型別   | 說明 |
|----------------|--------|------|
| `user_db_id`   | number | 資料庫主鍵 |
| `user_id`      | string | 對外使用者 ID |
| `peer_id`      | string | Peer ID |
| `display_name` | string | 顯示名稱 |

### 常見錯誤

| HTTP | 說明 |
|------|------|
| `405` | 非 POST：`error` 為 `method not allowed` |
| `400` | JSON/Base64/欄位缺失/`node_peer_id` 不符：見 `error` 字串（如 `nonce must be standard base64`、`node_peer_id does not match this server`） |
| `401` | 驗證失敗：`error` 為伺服器回傳的錯誤訊息（例如 `challenge expired`、`peer id mismatch`、`invalid challenge signature`、`consume challenge nonce: ...` 等，與 `VerifyChallenge` 一致） |
| `500` | `token issue failed` 等 |

---

## 需 Bearer 的 v1 API（與 libp2p session 對齊）

以下路由需標頭 **`Authorization: Bearer <access_token>`**（見上文 JWT）。行為與 libp2p stream 上對應訊息類型一致，JSON 欄位命名與 proto `meshserver.session.v1` 中結構的 `json` 標籤一致（如 `space_id`、`channel_id`、`message_type` 等）。

| 方法 | 路徑 | 對應 libp2p |
|------|------|-------------|
| GET | `/v1/me` | （無；回傳目前使用者資料庫列） |
| GET | `/v1/spaces` | `LIST_SPACES_*` |
| POST | `/v1/spaces` | `CREATE_SPACE_*`（全站管理員） |
| GET | `/v1/permissions/create-space` | `GET_CREATE_SPACE_PERMISSIONS_*` |
| POST | `/v1/spaces/{space_id}/join` | `JOIN_SPACE_*` |
| GET | `/v1/spaces/{space_id}/permissions/create-group` | `GET_CREATE_GROUP_PERMISSIONS_*` |
| GET | `/v1/spaces/{space_id}/members` | `LIST_SPACE_MEMBERS_*` |
| PATCH | `/v1/spaces/{space_id}/members/{target_user_id}/role` | `ADMIN_SET_SPACE_MEMBER_ROLE_*` |
| PUT | `/v1/spaces/{space_id}/settings/channel_creation` | `ADMIN_SET_SPACE_CHANNEL_CREATION_*` |
| POST | `/v1/spaces/{space_id}/invitations` | `INVITE_SPACE_MEMBER_*` |
| POST | `/v1/spaces/{space_id}/kick` | `KICK_SPACE_MEMBER_*` |
| POST | `/v1/spaces/{space_id}/ban` | `BAN_SPACE_MEMBER_*` |
| POST | `/v1/spaces/{space_id}/unban` | `UNBAN_SPACE_MEMBER_*` |
| GET | `/v1/spaces/{space_id}/channels` | `LIST_CHANNELS_*` |
| POST | `/v1/spaces/{space_id}/channels` | `CREATE_CHANNEL_*`（廣播頻道） |
| POST | `/v1/spaces/{space_id}/groups` | `CREATE_GROUP_*`（群組頻道） |
| POST | `/v1/channels/{channel_id}/messages` | `SEND_MESSAGE_*` |
| GET | `/v1/channels/{channel_id}/sync` | `SYNC_CHANNEL_*` |
| POST | `/v1/channels/{channel_id}/delivered_ack` | `CHANNEL_DELIVER_ACK` |
| POST | `/v1/channels/{channel_id}/read` | `CHANNEL_READ_UPDATE` |
| PUT | `/v1/channels/{channel_id}/settings/auto_delete` | `ADMIN_SET_GROUP_AUTO_DELETE_*` |
| GET | `/v1/media/{media_id}` | `GET_MEDIA_*` |
| POST | `/v1/media` | 上傳附件（取得 `media_id`） |

`{space_id}`、`{channel_id}` 為十進位整數；`{media_id}`、`{target_user_id}` 為字串（路徑內請 URL 編碼）。

**未提供 HTTP 的 libp2p 能力**：`SUBSCRIBE_CHANNEL` / `UNSUBSCRIBE_CHANNEL` 僅綁定 libp2p stream 以接收即時 `MESSAGE_EVENT`；HTTP 客戶端請用 **`GET /v1/channels/{channel_id}/sync`** 輪詢，或自行接 WebSocket/SSE（本專案尚未實作）。

未帶或無效的 Bearer：HTTP **`401`**，body `{"error":"authentication required"}`。

---

### `GET /v1/me`

回傳目前 JWT 對應使用者的資料庫資訊。

#### 成功 `200` body

| 欄位 | 型別 | 說明 |
|------|------|------|
| `user_db_id` | number | 資料庫主鍵 |
| `user_id` | string | 對外使用者 ID |
| `peer_id` | string | Peer ID |
| `display_name` | string | 顯示名稱 |
| `avatar_url` | string | 頭像 URL |
| `bio` | string | 簡介 |
| `last_login_at` | string | RFC3339Nano，UTC |

#### 錯誤

| HTTP | 說明 |
|------|------|
| `404` | 使用者不存在 |
| `500` | `load user failed` |

---

### `GET /v1/spaces`

#### 成功 `200` body

| 欄位 | 型別 | 說明 |
|------|------|------|
| `spaces` | array | 與 `ListSpacesResp.spaces` 相同元素結構（`SpaceSummary`） |

---

### `POST /v1/spaces/{space_id}/join`

加入指定 Space（與 `JOIN_SPACE_REQ` 相同，`space_id` 以路徑為準）。無需 body。

#### 成功 `200` body

與 `JoinSpaceResp` 對齊：

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | 固定 `true` |
| `space_id` | number | |
| `space` | object | `SpaceSummary` |
| `message` | string | 如 `joined` |

---

### `GET /v1/spaces/{space_id}/members`

列出 Space 成員（與 `LIST_SPACE_MEMBERS_REQ` 相同）。**僅 owner 或 admin** 可呼叫；否則 HTTP **`403`**，`error` 為 `admin role required`。

#### 查詢參數

| 參數 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `after_member_id` | number | 否 | 分頁游標（僅回傳大於此 member id 的項目）；預設 `0` |
| `limit` | number | 否 | 每頁筆數；`0` 則預設 **20**，最大 **100** |

#### 成功 `200` body

與 `ListSpaceMembersResp` 對齊（`space_id`、`members`、`next_after_member_id`、`has_more`）。

---

### `GET /v1/spaces/{space_id}/channels`

#### 成功 `200` body

| 欄位 | 型別 | 說明 |
|------|------|------|
| `space_id` | number | 路徑參數回顯 |
| `channels` | array | 與 `ListChannelsResp.channels` 相同（`ChannelSummary`） |

---

### `POST /v1/channels/{channel_id}/messages`

請求 body 與 `SendMessageReq` 相同，但 **`channel_id` 以路徑為準**（body 若帶 `channel_id` 應與路徑一致；實作以路徑覆寫）。

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `client_msg_id` | string | 否 | 客戶端訊息去重 ID |
| `message_type` | number | 否 | 與 proto `MessageType` 列舉一致（0=未指定則依內容推斷） |
| `content` | object | 否 | 與 `MessageContent` 相同：`text`、`images[]`、`files[]`（內嵌位元組欄位為 JSON 陣列形式之數字，與 proto JSON 一致） |

請求體上限 **8 MiB**（含內嵌附件）。

#### 成功 `200` body

與 `SendMessageAck` 語意對齊：

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | 固定 `true` |
| `channel_id` | number | |
| `client_msg_id` | string | |
| `message_id` | string | |
| `seq` | number | |
| `server_time_ms` | number | 伺服器處理時間（Unix 毫秒） |
| `message` | string | 如 `stored` |

#### 錯誤

- **`403`**：`forbidden`（與 `ErrForbidden` 一致，例如非成員）
- **`400`**：驗證或業務錯誤（訊息內容無效等）
- 其他錯誤多為 **`400`** 並帶 `error` 字串

---

### `GET /v1/channels/{channel_id}/sync`

查詢參數：

| 參數 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `after_seq` | number | 否 | 僅取得序號大於此值的訊息；預設 `0` |
| `limit` | number | 否 | 筆數上限；`0` 或未傳則使用伺服器 `default_sync_limit` / 上限邏輯（與 `SyncChannel` 一致） |

#### 成功 `200` body

與 `SyncChannelResp` 對齊：

| 欄位 | 型別 | 說明 |
|------|------|------|
| `channel_id` | number | |
| `messages` | array | `MessageEvent` 列表 |
| `next_after_seq` | number | 下次同步游標 |
| `has_more` | bool | 是否還有資料 |

#### 錯誤

- **`403`**：非頻道成員等
- **`404`**：資源不存在（若底層回傳 `ErrNotFound`）

---

### `GET /v1/media/{media_id}`

下載媒體內容（與 `GET_MEDIA_REQ` / `GET_MEDIA_RESP` 相同）。使用者須對該 `media_id` 關聯之**至少一個頻道**具備可檢視權限，否則 **`403`**。

#### 成功 `200` body

與 `GetMediaResp` 對齊：`ok`、`media_id`、`file`（`MediaFile`，含 `inline_data` 為 Base64 編碼之 JSON 字串，與標準 `encoding/json` 對 `[]byte` 序列化一致）、`message`。

大檔建議之後可改走 blob HTTP 靜態路徑；目前實作與 libp2p 一併回傳 JSON 內嵌位元組。

---

### `POST /v1/media`

以 **`multipart/form-data`** 上傳檔案，寫入 blob 並建立 media 記錄（與訊息附件流程一致）。需 Bearer；受 **`max_upload_bytes` / `MESHSERVER_MAX_UPLOAD_BYTES`** 約束。

僅在程序已注入 `MediaService` 且 `MaxUploadBytes > 0` 時註冊此路由。

#### 表單欄位

| 欄位 | 必填 | 說明 |
|------|------|------|
| `file` | 是 | 檔案本體 |
| `kind` | 否 | `image` 或 `file`（預設 `file`） |
| `original_name` | 否 | 原始檔名；未填則使用上傳檔名 |
| `mime_type` | 否 | MIME；未填則使用 part 的 `Content-Type` |

#### 成功 `200` body

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `media_id` | string | 寫入訊息 `content.images[].media_id` / `files[].media_id` 所用 |
| `blob_id` | string | |
| `sha256` | string | |
| `mime_type` | string | |
| `size` | number | |
| `original_name` | string | |
| `kind` | string | `image` 或 `file` |
| `message` | string | 如 `stored` |

#### 錯誤

| HTTP | 說明 |
|------|------|
| `413` | 超過單檔上傳上限 |
| `400` | 缺 `file`、multipart 解析失敗、或 `kind` 非法 |

---

### 其餘 v1 端點（摘要）

以下成功回應 body 皆與對應 proto 訊息一致（欄位見 `session.proto` / 產生之 `*.pb.go` 的 `json` 標籤）。

| 端點 | 說明 |
|------|------|
| `POST /v1/spaces` | Body：`CreateSpaceReq`。僅 **`MESHSERVER_DEFAULT_ADMIN_PEER_ID`** 對應之 Peer 可建立；否則 **`403`** `create space permission required`。 |
| `GET /v1/permissions/create-space` | 回傳 `GetCreateSpacePermissionsResp`（`can_create_space`）。 |
| `GET /v1/spaces/{space_id}/permissions/create-group` | 回傳 `GetCreateGroupPermissionsResp`。 |
| `PATCH .../members/{target_user_id}/role` | Body：`{"role": <MemberRole 數值>}`。變更為 owner 僅現任 owner 可執行，否則 **`403`** `owner role required`；需 admin/owner 時不符則 **`403`** `admin role required`。 |
| `PUT .../settings/channel_creation` | Body：`{"allow_channel_creation": bool}`。需 space admin/owner。 |
| `PUT /v1/channels/{channel_id}/settings/auto_delete` | Body：`{"auto_delete_after_seconds": number}`。僅 **群組（group）** 頻道；否則 **`400`** `auto delete is only supported for group channels`。 |
| `POST .../invitations`、`kick`、`ban`、`unban` | Body：`{"target_user_id": "<對外 user_id>"}`。需 admin/owner（除另有規則）。 |
| `POST .../groups` | Body：`CreateGroupReq`（`space_id` 以路徑為準並覆寫 body）。 |
| `POST .../channels`（建立） | Body：`CreateChannelReq`（廣播頻道，`space_id` 以路徑為準）。 |
| `POST .../delivered_ack` | Body：`{"acked_seq": number}`。成功 `200`：`{"ok": true}`。 |
| `POST .../read` | Body：`{"last_read_seq": number}`。成功 `200`：`{"ok": true}`。 |

#### 權限相關 `error` 字串（常見）

| 字串 | 典型 HTTP |
|------|-----------|
| `admin role required` | `403` |
| `owner role required` | `403` |
| `create space permission required` | `403` |
| `forbidden` | `403` |

---

## 其他內建路由（非 v1 auth）

以下路由由同一 HTTP 伺服器提供，供運維或除錯使用。

| 方法 | 路徑 | 說明 |
|------|------|------|
| GET | `/healthz` | 存活探測；`200` 時 body 含 `status`、`time`（RFC3339Nano） |
| GET | `/readyz` | 就緒探測；未就緒時 `503` 與 `{"status":"not_ready"}` |
| GET | `/version` | 回傳 `version` 字串 |
| GET | `/debug/config` | 僅當 `enable_debug_config` 為真且已設定快照回呼時；回傳設定快照 JSON |
| GET | `/blobs/*` | 僅當 `serve_blobs_over_http` 等條件滿足時；靜態檔案服務 |

---

## 相關程式位置

- Challenge / 簽名 payload：`internal/auth/service.go`（`BuildChallengePayload`、`IssueChallenge`、`VerifyChallenge`）
- HTTP 路由與 handler：`internal/api/auth_http.go`、`internal/api/v1_http.go`、`internal/api/v1_http_rest.go`
- Session 與 libp2p 共用邏輯：`internal/session/manager.go`、`internal/session/manager_v1_rest.go`（`CreateSpaceForAPI`、`GetCreateGroupPermissionsForAPI`、成員管理等）及 `internal/session/errors.go`
- JWT：`internal/api/jwt.go`
- 設定鍵與環境變數：`internal/config/config.go`
