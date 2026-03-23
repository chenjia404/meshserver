# meshserver HTTP API

本文件描述管理用 HTTP 服務的 JSON API。一般都是配置一个https的网站域名，所有请求路径都基于这个域名，WebSocket用wss协议。

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

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/auth/challenge` |
| 請求 Header | `Content-Type: application/json`；**不需要** Bearer |
| 請求 body | 見下表 |

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

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/auth/verify` |
| 請求 Header | `Content-Type: application/json`；**不需要** Bearer |
| 請求 body | 見下表 |

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

## 資料型別與列舉（v1 JSON）

以下為 HTTP JSON 與 proto `meshserver.session.v1` 對齊之**欄位名**（`json` 標籤，多為 `snake_case`）。數字型列舉在 JSON 中為 **number**。

### 列舉值

| 型別 | 值（number） | 常數名（對照用） |
|------|----------------|------------------|
| `Visibility` | `0` / `1` / `2` | `VISIBILITY_UNSPECIFIED` / `PUBLIC` / `PRIVATE` |
| `ChannelType` | `0` / `1` / `2` | `CHANNEL_TYPE_UNSPECIFIED` / `GROUP`（群組） / `BROADCAST`（廣播頻道） |
| `MessageType` | `0`…`4` | `UNSPECIFIED` / `TEXT` / `IMAGE` / `FILE` / `SYSTEM` |
| `MemberRole` | `0`…`4` | `UNSPECIFIED` / `OWNER` / `ADMIN` / `MEMBER` / `SUBSCRIBER` |

### `SpaceSummary`（物件）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `space_id` | number | Space 業務 ID |
| `name` | string | 名稱 |
| `avatar_url` | string | 頭像 URL |
| `description` | string | 描述 |
| `visibility` | number | 見 `Visibility` |
| `member_count` | number | 成員數 |
| `allow_channel_creation` | bool | 是否允許在該 Space 內建立頻道（實際顯示值含全站管理員規則） |

### `ChannelSummary`（物件）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `channel_id` | number | 頻道 ID |
| `space_id` | number | 所屬 Space ID |
| `type` | number | 見 `ChannelType` |
| `name` | string | 名稱 |
| `description` | string | 描述 |
| `visibility` | number | 見 `Visibility` |
| `slow_mode_seconds` | number | 慢速模式秒數 |
| `auto_delete_after_seconds` | number | 群組自動刪除秒數（0 表示關閉等，依實作） |
| `last_seq` | number | 目前訊息序號游標 |
| `member_count` | number | 成員數 |
| `can_view` | bool | 當前使用者是否可檢視 |
| `can_send_message` | bool | 是否可發文字 |
| `can_send_image` | bool | 是否可發圖 |
| `can_send_file` | bool | 是否可發檔 |

### `SpaceMemberSummary`（物件）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `member_id` | number | 成員記錄 ID（分頁用） |
| `user_id` | string | 對外使用者 ID |
| `display_name` | string | 顯示名稱 |
| `avatar_url` | string | 頭像 |
| `role` | number | 見 `MemberRole` |
| `nickname` | string | 暱稱 |
| `is_muted` | bool | 是否禁言 |
| `is_banned` | bool | 是否封禁 |
| `joined_at_ms` | number | 加入時間 Unix 毫秒 |
| `last_seen_at_ms` | number | 最後活躍毫秒（0 可能表示無） |

### `MediaImage` / `MediaFile`（物件）

`MediaImage`：

| 欄位 | 型別 | 說明 |
|------|------|------|
| `media_id` | string | 媒體 ID |
| `blob_id` | string | Blob ID |
| `sha256` | string | 雜湊 |
| `url` | string | 可訪問 URL（若已設定 blob 基底） |
| `width` / `height` | number | 像素（圖） |
| `mime_type` | string | MIME |
| `size` | number | 位元組大小 |
| `inline_data` | string | Base64（JSON 對 `[]byte` 序列化；僅在請求內嵌或部分回應出現） |
| `original_name` | string | 原始檔名 |

`MediaFile`：

| 欄位 | 型別 | 說明 |
|------|------|------|
| `media_id` | string | |
| `blob_id` | string | |
| `sha256` | string | |
| `file_name` | string | 檔名（proto 欄位名） |
| `url` | string | 可訪問 URL |
| `mime_type` | string | |
| `size` | number | |
| `inline_data` | string | Base64（同上） |

### `MessageContent`（物件）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `text` | string | 文字內容 |
| `images` | array | 元素為 `MediaImage` |
| `files` | array | 元素為 `MediaFile` |

### `MessageEvent`（物件；sync / WebSocket 推送）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `channel_id` | number | 頻道 ID |
| `message_id` | string | 訊息 UUID/業務 ID |
| `seq` | number | 頻道內序號 |
| `sender_user_id` | string | 發送者對外 `user_id` |
| `message_type` | number | 見 `MessageType` |
| `content` | object | `MessageContent`，可為 null |
| `created_at_ms` | number | 建立時間 Unix 毫秒 |

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
| GET | `/v1/ws` | WebSocket：訂閱頻道後推送 **`MESSAGE_EVENT` 等價 JSON**（含完整訊息內容，見下節） |

`{space_id}`、`{channel_id}` 為十進位整數；`{media_id}`、`{target_user_id}` 為字串（路徑內請 URL 編碼）。

**libp2p 與 HTTP 對照**：`SUBSCRIBE_CHANNEL` / `UNSUBSCRIBE_CHANNEL` 在 stream 上註冊即時推送；HTTP 端請使用 **`GET /v1/ws`** 送出 `subscribe` / `unsubscribe`，或僅用 **`GET /v1/channels/{channel_id}/sync`** 輪詢。

未帶或無效的 Bearer：HTTP **`401`**，body `{"error":"authentication required"}`（WebSocket 升級失敗時同樣回 JSON，不升級）。

---

### `GET /v1/me`

回傳目前 JWT 對應使用者的資料庫資訊。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/me` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |
| 請求 body | 無 |

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
| `401` | 未帶或無效 Bearer |
| `404` | 使用者不存在 |
| `500` | `load user failed` |

---

### `GET /v1/spaces`

列出當前使用者可見的 Space（`LIST_SPACES_*`）。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/spaces` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |
| 請求 body | 無 |

#### 成功 `200` body

| 欄位 | 型別 | 說明 |
|------|------|------|
| `spaces` | array | 元素為 `SpaceSummary`（欄位見上文「資料型別與列舉」） |

---

### `POST /v1/spaces`

建立新 Space（`CREATE_SPACE_*`）。**僅** `MESHSERVER_DEFAULT_ADMIN_PEER_ID` 對應之 Peer 可建立；否則 **`403`**，`error` 為 `create space permission required`。伺服器建立時會將 `allow_channel_creation` 設為 `true`（body 中的 `allow_channel_creation` 不影響此行為）。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/spaces` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |
| 請求 body | 見下表 |

#### 請求 body（`CreateSpaceReq`）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `name` | string | 是 | Space 名稱 |
| `description` | string | 否 | 描述 |
| `visibility` | number | 否 | 見 `Visibility` |
| `allow_channel_creation` | bool | 否 | 可忽略（伺服器仍設為允許建立） |

#### 成功 `200` body（`CreateSpaceResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | 新建 Space ID |
| `space` | object | `SpaceSummary` |
| `message` | string | 如 `created` |

---

### `GET /v1/permissions/create-space`

查詢當前使用者是否可建立 Space（`GET_CREATE_SPACE_PERMISSIONS_*`）。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/permissions/create-space` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |
| 請求 body | 無 |

#### 成功 `200` body（`GetCreateSpacePermissionsResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `can_create_space` | bool | 是否為全站管理員（可建 Space） |
| `message` | string | 如 `ok` |

---

### `GET /v1/spaces/{space_id}/permissions/create-group`

查詢在指定 Space 內建立群組頻道之權限（`GET_CREATE_GROUP_PERMISSIONS_*`）。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/spaces/{space_id}/permissions/create-group` |
| 路徑參數 | `space_id`（number） |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |
| 請求 body | 無 |

#### 成功 `200` body（`GetCreateGroupPermissionsResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | 與路徑一致 |
| `space` | object | `SpaceSummary` |
| `role` | number | 當前使用者在該 Space 的 `MemberRole` |
| `can_create_group` | bool | 是否可建立群組頻道 |
| `message` | string | 如 `ok` |

---

### `POST /v1/spaces/{space_id}/join`

加入指定 Space（`JOIN_SPACE_*`）。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/spaces/{space_id}/join` |
| 路徑參數 | `space_id`（number） |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |
| 請求 body | 無 |

#### 成功 `200` body（`JoinSpaceResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | 與路徑一致 |
| `space` | object | `SpaceSummary` |
| `message` | string | 如 `joined` |

---

### `GET /v1/spaces/{space_id}/members`

列出 Space 成員（`LIST_SPACE_MEMBERS_*`）。**僅 owner 或 admin**；否則 **`403`**，`error` 為 `admin role required`。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/spaces/{space_id}/members` |
| 路徑參數 | `space_id`（number） |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |

#### Query 參數

| 參數 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `after_member_id` | number | 否 | 分頁游標；預設 `0` |
| `limit` | number | 否 | 每頁筆數；`0` 則預設 **20**，最大 **100** |

#### 成功 `200` body（`ListSpaceMembersResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `space_id` | number | 與路徑一致 |
| `members` | array | 元素為 `SpaceMemberSummary` |
| `next_after_member_id` | number | 下一頁游標；無更多時可為 `0` |
| `has_more` | bool | 是否還有下一頁 |

---

### `PATCH /v1/spaces/{space_id}/members/{target_user_id}/role`

變更成員角色（`ADMIN_SET_SPACE_MEMBER_ROLE_*`）。將目標設為 **owner** 僅現任 **owner** 可執行（否則 **`403`** `owner role required`）；其餘角色變更需要 **owner 或 admin**（否則 **`403`** `admin role required`）。

| 項目 | 說明 |
|------|------|
| 方法 | `PATCH` |
| 路徑 | `/v1/spaces/{space_id}/members/{target_user_id}/role` |
| 路徑參數 | `space_id`（number）、`target_user_id`（string，對外 user_id，URL 編碼） |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |

#### 請求 body（`AdminSetSpaceMemberRoleReq` 路徑欄位以外）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `role` | number | 是 | `MemberRole` |

#### 成功 `200` body（`AdminSetSpaceMemberRoleResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | |
| `target_user_id` | string | 與路徑一致 |
| `role` | number | 設定後的 `MemberRole` |
| `message` | string | 如 `updated` |

---

### `PUT /v1/spaces/{space_id}/settings/channel_creation`

設定 Space 是否允許成員建立頻道（`ADMIN_SET_SPACE_CHANNEL_CREATION_*`）。需 **owner 或 admin**。

| 項目 | 說明 |
|------|------|
| 方法 | `PUT` |
| 路徑 | `/v1/spaces/{space_id}/settings/channel_creation` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |

#### 請求 body（`AdminSetSpaceChannelCreationReq`）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `allow_channel_creation` | bool | 是 | 是否允許 |

#### 成功 `200` body（`AdminSetSpaceChannelCreationResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | |
| `allow_channel_creation` | bool | 目前值 |
| `message` | string | 如 `updated` |

---

### `POST /v1/spaces/{space_id}/invitations`

邀請使用者加入 Space（`INVITE_SPACE_MEMBER_*`）。需 **owner 或 admin**。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/spaces/{space_id}/invitations` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |

#### 請求 body（`InviteSpaceMemberReq`）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `target_user_id` | string | 是 | 被邀請者對外 `user_id` |

#### 成功 `200` body（`InviteSpaceMemberResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | |
| `target_user_id` | string | |
| `space` | object | `SpaceSummary` |
| `message` | string | 如 `invited` |

---

### `POST /v1/spaces/{space_id}/kick`

踢出成員（`KICK_SPACE_MEMBER_*`）。需 **owner 或 admin**。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/spaces/{space_id}/kick` |
| 請求 body | 與邀請相同：`{"target_user_id": string}`（`KickSpaceMemberReq`） |

#### 成功 `200` body（`KickSpaceMemberResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | |
| `target_user_id` | string | |
| `space` | object | `SpaceSummary` |
| `message` | string | 如 `kicked` |

---

### `POST /v1/spaces/{space_id}/ban`

封禁成員（`BAN_SPACE_MEMBER_*`）。需 **owner 或 admin**。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/spaces/{space_id}/ban` |
| 請求 body | `{"target_user_id": string}`（`BanSpaceMemberReq`） |

#### 成功 `200` body（`BanSpaceMemberResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | |
| `target_user_id` | string | |
| `space` | object | `SpaceSummary` |
| `message` | string | 如 `banned` |

---

### `POST /v1/spaces/{space_id}/unban`

解除封禁（`UNBAN_SPACE_MEMBER_*`）。需 **owner 或 admin**。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/spaces/{space_id}/unban` |
| 請求 body | `{"target_user_id": string}`（`UnbanSpaceMemberReq`） |

#### 成功 `200` body（`UnbanSpaceMemberResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | |
| `target_user_id` | string | |
| `space` | object | `SpaceSummary` |
| `message` | string | 如 `unbanned` |

---

### `GET /v1/spaces/{space_id}/channels`

列出 Space 下頻道（`LIST_CHANNELS_*`）。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/spaces/{space_id}/channels` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |
| 請求 body | 無 |

#### 成功 `200` body（`ListChannelsResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `space_id` | number | 與路徑一致 |
| `channels` | array | 元素為 `ChannelSummary` |

---

### `POST /v1/spaces/{space_id}/channels`

建立**廣播**頻道（`CREATE_CHANNEL_*`，`ChannelType_BROADCAST`）。`space_id` 以路徑為準並覆寫 body 中的 `space_id`。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/spaces/{space_id}/channels` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |

#### 請求 body（`CreateChannelReq`）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `space_id` | number | 否 | 可省略，以路徑為準 |
| `name` | string | 是 | 頻道名稱 |
| `description` | string | 否 | 描述 |
| `visibility` | number | 否 | `Visibility` |
| `slow_mode_seconds` | number | 否 | 慢速秒數 |

#### 成功 `200` body（`CreateChannelResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | |
| `channel_id` | number | 新建頻道 ID |
| `channel` | object | `ChannelSummary` |
| `message` | string | 如 `created` |

---

### `POST /v1/spaces/{space_id}/groups`

建立**群組**頻道（`CREATE_GROUP_*`，`ChannelType_GROUP`）。`space_id` 以路徑為準並覆寫 body。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/spaces/{space_id}/groups` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |

#### 請求 body（`CreateGroupReq`）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `space_id` | number | 否 | 以路徑為準 |
| `name` | string | 是 | 群組名稱 |
| `description` | string | 否 | 描述 |
| `visibility` | number | 否 | `Visibility` |
| `slow_mode_seconds` | number | 否 | 慢速秒數 |

#### 成功 `200` body（`CreateGroupResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `space_id` | number | |
| `channel_id` | number | 新建頻道 ID |
| `channel` | object | `ChannelSummary` |
| `message` | string | 如 `created` |

---

### `POST /v1/channels/{channel_id}/messages`

發送訊息（`SEND_MESSAGE_*`）。**路徑中的 `channel_id` 會覆寫 body 內 `channel_id`**。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/channels/{channel_id}/messages` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |
| 請求體上限 | **8 MiB** |

#### 請求 body（`SendMessageReq`，`channel_id` 以路徑為準）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `channel_id` | number | 否 | 忽略，以路徑為準 |
| `client_msg_id` | string | 否 | 客戶端去重 ID |
| `message_type` | number | 否 | `MessageType`；`0` 時依內容推斷 |
| `content` | object | 否 | `MessageContent`：`text`、`images[]`（`MediaImage`）、`files[]`（`MediaFile`）；內嵌二進位請用 Base64 字串作為 `inline_data` |

#### 成功 `200` body（`SendMessageAck`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `channel_id` | number | |
| `client_msg_id` | string | |
| `message_id` | string | 伺服器訊息 ID |
| `seq` | number | 頻道序號 |
| `server_time_ms` | number | Unix 毫秒 |
| `message` | string | 如 `stored` |

#### 錯誤

| HTTP | 說明 |
|------|------|
| `401` | 未授權 |
| `403` | `forbidden`（非成員等） |
| `400` | 驗證/業務錯誤，`error` 字串 |

---

### `GET /v1/channels/{channel_id}/sync`

歷史同步（`SYNC_CHANNEL_*`）。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/channels/{channel_id}/sync` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |

#### Query 參數（`SyncChannelReq`）

| 參數 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `after_seq` | number | 否 | 僅取 `seq` 大於此值之訊息；預設 `0` |
| `limit` | number | 否 | 筆數；`0` 或未傳則用伺服器 `default_sync_limit` 與 `max_sync_limit` |

#### 成功 `200` body（`SyncChannelResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `channel_id` | number | |
| `messages` | array | 元素為 `MessageEvent`（見「資料型別與列舉」） |
| `next_after_seq` | number | 下次請求建議帶入之 `after_seq` |
| `has_more` | bool | 是否還有未拉取資料 |

#### 錯誤

| HTTP | 說明 |
|------|------|
| `403` | 非成員等 |
| `404` | 資源不存在（若底層 `ErrNotFound`） |

---

### `POST /v1/channels/{channel_id}/delivered_ack`

送達確認（`CHANNEL_DELIVER_ACK`）。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/channels/{channel_id}/delivered_ack` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |

#### 請求 body（`ChannelDeliverAck`）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `acked_seq` | number | 是 | 已送達之最大 `seq` |

#### 成功 `200` body

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |

---

### `POST /v1/channels/{channel_id}/read`

更新已讀游標（`CHANNEL_READ_UPDATE`）。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/channels/{channel_id}/read` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |

#### 請求 body（`ChannelReadUpdate`）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `last_read_seq` | number | 是 | 已讀至該 `seq` |

#### 成功 `200` body

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |

---

### `PUT /v1/channels/{channel_id}/settings/auto_delete`

設定群組自動刪除（`ADMIN_SET_GROUP_AUTO_DELETE_*`）。**僅群組（`ChannelType` = GROUP）**；否則 **`400`** `auto delete is only supported for group channels`。需 **Space 之 owner 或 admin**。

| 項目 | 說明 |
|------|------|
| 方法 | `PUT` |
| 路徑 | `/v1/channels/{channel_id}/settings/auto_delete` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: application/json` |

#### 請求 body（`AdminSetGroupAutoDeleteReq`）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `auto_delete_after_seconds` | number | 是 | 秒；`0` 通常表示關閉（依實作） |

#### 成功 `200` body（`AdminSetGroupAutoDeleteResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `channel_id` | number | |
| `auto_delete_after_seconds` | number | 更新後值 |
| `channel` | object | `ChannelSummary` |
| `message` | string | 如 `updated` |

---

### `GET /v1/media/{media_id}`

下載媒體（`GET_MEDIA_*`）。須對該 `media_id` 所關聯之任一頻道具 **can_view**，否則 **`403`**。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/media/{media_id}` |
| 路徑參數 | `media_id`（string） |
| 請求 Header | `Authorization: Bearer <access_token>`（必填） |
| 請求 body | 無 |

#### 成功 `200` body（`GetMediaResp`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `media_id` | string | |
| `file` | object | `MediaFile`；`inline_data` 在 JSON 中為 **Base64 字串**（`encoding/json` 序列化 `[]byte`） |
| `message` | string | 如 `ok` |

---

### `POST /v1/media`

以 **`multipart/form-data`** 上傳檔案。需 Bearer；單檔受 **`max_upload_bytes`** 限制。僅在已註冊 `MediaService` 且 `MaxUploadBytes > 0` 時可用。

| 項目 | 說明 |
|------|------|
| 方法 | `POST` |
| 路徑 | `/v1/media` |
| 請求 Header | `Authorization: Bearer <access_token>`（必填）；`Content-Type: multipart/form-data`（含 boundary） |

#### 表單欄位

| 欄位 | 必填 | 說明 |
|------|------|------|
| `file` | 是 | 檔案本體 |
| `kind` | 否 | `image` 或 `file`（預設 `file`） |
| `original_name` | 否 | 原始檔名；未填則用 part 檔名 |
| `mime_type` | 否 | 未填則用 part 的 `Content-Type` |

#### 成功 `200` body

| 欄位 | 型別 | 說明 |
|------|------|------|
| `ok` | bool | `true` |
| `media_id` | string | 供 `SendMessage` 引用 |
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
| `413` | 超過上傳上限 |
| `400` | 缺 `file`、multipart 錯誤、`kind` 非法 |

---

### 權限相關 `error` 字串（常見）

| 字串 | 典型 HTTP |
|------|-----------|
| `admin role required` | `403` |
| `owner role required` | `403` |
| `create space permission required` | `403` |
| `forbidden` | `403` |

---

## WebSocket `GET /v1/ws`

與 libp2p 上 `MESSAGE_EVENT` 使用**同一套** `toMessageEvent`：`event` 物件欄位與 **`MessageEvent`**（見「資料型別與列舉」）及 **`GET /v1/channels/{channel_id}/sync`** 之 `messages[]` 元素一致，客戶端可直接解析，無需再 sync 該則。

| 項目 | 說明 |
|------|------|
| 方法 | `GET` |
| 路徑 | `/v1/ws` |
| 協議 | 一般站點為 **`wss://`**；升級 WebSocket |
| 認證（擇一） | ① Header `Authorization: Bearer <access_token>` ② Query `?access_token=<token>` |
| 升級失敗 | **401**，body：`{"error":"authentication required"}`（不完成握手） |

### 客戶端 → 伺服器（JSON 文字訊息）

每則訊息為 UTF-8 JSON 物件，單次讀取大小上限約 **64 KiB**。

#### 請求欄位（共用）

| 欄位 | 型別 | 必填 | 說明 |
|------|------|------|------|
| `action` | string | 是 | `subscribe` / `unsubscribe` / `ping`（大小寫不敏感） |
| `channel_id` | number | 視 action | `subscribe` / `unsubscribe` 必填且非 0 |

| `action` | 行為 |
|----------|------|
| `subscribe` | 訂閱頻道；須為成員，否則見下行「錯誤」 |
| `unsubscribe` | 取消訂閱 |
| `ping` | 心跳，見下行「控制回應」 |

### 伺服器 → 客戶端

#### 控制與確認（非新訊息）

| `type` | 其他欄位 | 說明 |
|--------|----------|------|
| `pong` | `message`（string，如 `ok`） | 回應 `action: ping` |
| `subscribed` | `channel_id`、`message` | 訂閱成功 |
| `unsubscribed` | `channel_id`、`message` | 取消訂閱成功 |
| `error` | `error`（string） | 業務或格式錯誤（見下行） |

`type` 為 `error` 時，`error` 欄位可能為：`invalid json`、`channel_id required`、`not a channel member`、`unknown action`。

#### 新訊息（`message_event`）

| 欄位 | 型別 | 說明 |
|------|------|------|
| `type` | string | 固定 `message_event` |
| `channel_id` | number | 頻道 ID |
| `event` | object | 完整 **`MessageEvent`**（`channel_id`、`message_id`、`seq`、`sender_user_id`、`message_type`、`content`、`created_at_ms`） |

### 其它說明

- 伺服器會定期發送 **WebSocket Ping** 幀；客戶端應回 **Pong**（瀏覽器通常自動處理）。
- 發送緩衝滿時可能丟棄 `message_event` 並寫 log。
- 實作：`internal/api/ws.go`、`internal/session/realtime.go`。

---

## 其他內建路由（非 v1 auth）

以下路由**不需要** Bearer（除非另有說明）。

### `GET /healthz`

| 項目 | 說明 |
|------|------|
| 請求 | 無 body |
| **200** body | `status`（string，如 `ok`）、`time`（string，RFC3339Nano，UTC） |

### `GET /readyz`

| 項目 | 說明 |
|------|------|
| 請求 | 無 body |
| **200** body | `status`（string，如 `ready`） |
| **503** body | `status`（string，如 `not_ready`） |

### `GET /version`

| 項目 | 說明 |
|------|------|
| 請求 | 無 body |
| **200** body | `version`（string，版本字串或 `unknown`） |

### `GET /debug/config`

| 項目 | 說明 |
|------|------|
| 條件 | 設定 `enable_debug_config` 為真且伺服器已註冊設定快照 |
| 請求 | 無 body |
| **200** body | 設定物件（結構同執行時 `Config` 快照，欄位依版本而定） |
| 未啟用 | 路由可能未註冊（**404**） |

### `GET /blobs/{相對路徑}`

| 項目 | 說明 |
|------|------|
| 條件 | `serve_blobs_over_http` 等為真且已設定 blob 根目錄 |
| 請求 | 路徑為 `/blobs/` 前綴後之檔案相對路徑；**非 JSON**，回傳檔案內容 |
| **200** | 檔案位元組，`Content-Type` 依檔案 |
| **403** | 路徑越權 |
| **404** | 檔案不存在 |

---

## 相關程式位置

- Challenge / 簽名 payload：`internal/auth/service.go`（`BuildChallengePayload`、`IssueChallenge`、`VerifyChallenge`）
- HTTP 路由與 handler：`internal/api/auth_http.go`、`internal/api/v1_http.go`、`internal/api/v1_http_rest.go`、`internal/api/ws.go`
- Session 與 libp2p 共用邏輯：`internal/session/manager.go`、`internal/session/manager_v1_rest.go`、`internal/session/realtime.go`（`DeliverMessage` 同時推 libp2p 與 WebSocket）
- JWT：`internal/api/jwt.go`
- 設定鍵與環境變數：`internal/config/config.go`
