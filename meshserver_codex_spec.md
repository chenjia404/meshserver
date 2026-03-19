# Meshspace 开发文档（给 Codex 直接生成 Go 代码）

## 1. 任务目标

请生成一个可运行的 **Go 1.26** 项目，实现一个基于 **libp2p** 的服务端程序，项目名暂定为 `meshspace`。服务端需要支持：

- 节点通过 **libp2p** 对外提供服务。
- 客户端通过 **libp2p Peer ID** 完成身份认证。
- 业务模型包含：
  - `space`：社区/空间
  - `Channel`：频道，仅支持两种类型：
    - `group`：群聊，支持普通聊天、图片、文件、文本
    - `channel`：广播频道，只有管理员可发消息，普通用户只能订阅和阅读
- 实时消息通过自定义二进制协议在 libp2p stream 上收发。
- 支持 `ACK / seq` 同步机制。
- 支持图片与文件附件。
- 所有二进制附件都要做 **SHA-256**，按内容去重存储。
- 项目必须自带一个 `docker-compose/` 目录，进入该目录后可以一键启动。
- 所有运行数据必须都挂载在 `docker-compose/` 目录下，不能散落到项目其他位置。

---

## 2. 技术硬约束

### 2.1 Go 版本

- 使用 **Go 1.26**。
- Docker 构建阶段也必须使用 Go 1.26。
- 代码需兼容 Linux 容器环境。

### 2.2 项目要求

- 使用 Go module。
- 代码结构清晰，按模块拆分。
- 优先可读性与可维护性，不要为了“炫技”过度抽象。
- 所有关键模块都要有明确注释。
- 所有配置都通过环境变量或配置文件注入。
- 生成的项目必须能直接 `docker compose up -d --build` 启动。

### 2.3 数据持久化要求

所有需要持久化的数据，都必须映射到项目中的：

```text
./docker-compose/data/
```

至少包含这些子目录：

```text
./docker-compose/data/mysql/
./docker-compose/data/blobs/
./docker-compose/data/logs/
./docker-compose/data/config/
```

说明：

- MySQL 数据文件放到 `docker-compose/data/mysql/`
- 附件/二进制 blob 文件放到 `docker-compose/data/blobs/`
- 日志文件放到 `docker-compose/data/logs/`
- 配置文件放到 `docker-compose/data/config/`

禁止把运行数据写到未挂载的容器内部目录中。

---

## 3. 交付结果要求

请生成完整项目代码，至少包括：

```text
meshspace/
  cmd/
    meshspace/
      main.go
  internal/
    app/
    auth/
    config/
    db/
    libp2p/
    protocol/
    space/
    channel/
    message/
    media/
    storage/
    session/
    service/
    repository/
    api/
    logx/
  migrations/
  proto/
  scripts/
  Dockerfile
  go.mod
  go.sum
  README.md
  .env.example
  docker-compose/
    docker-compose.yml
    .env.example
    data/
      mysql/
      blobs/
      logs/
      config/
```

要求：

- `docker-compose/docker-compose.yml` 必须存在。
- 在 `docker-compose/` 目录内执行以下命令即可完成启动：

```bash
docker compose up -d --build
```

- 必须提供 README，明确写出启动方式、默认端口、目录说明。
- 必须提供初始化 SQL 或迁移文件。

---

## 4. 系统总体架构

### 4.1 角色

#### 4.1.1 server

一个运行中的 `meshspace` 实例，本身是一个 libp2p 节点，对外监听 libp2p 连接。

#### 4.1.2 User

用户通过 libp2p Peer ID 进行身份认证。

第一版可以简单处理为：

- 一个 `peer_id` 对应一个 `user_id`

后续再扩展多设备模型。

#### 4.1.3 space

业务上的社区空间。

一个 server 可以托管多个 space。

#### 4.1.4 Channel

一个 space 下的子沟通单元，仅支持两种：

- `group`
- `channel`

---

## 5. 业务模型定义

### 5.1 space

表示一个社区。

字段建议：

- `space_id`
- `name`
- `description`
- `visibility`：`public | private`
- `owner_user_id`
- `member_count`
- `created_at`
- `updated_at`

### 5.2 Channel

#### 5.2.1 group

群聊。

能力：

- 允许成员发消息
- 支持文本
- 支持多图 + 文本
- 支持文件 + 文本
- 支持权限控制
- 支持公开/私密
- 支持慢速模式

#### 5.2.2 channel

广播频道。

能力：

- 只有管理员/owner 可发消息
- 普通用户只能订阅和阅读
- 支持文本
- 支持多图 + 文本
- 支持文件 + 文本
- 支持公开/私密

### 5.3 Message

消息属于某个 channel。

### 5.4 附件约束

不支持复杂图文混排。

一条消息的内容只允许以下三种之一：

1. 纯文本
2. 多图 + 一段文本（文本固定显示在最后）
3. 文件 + 一段文本（文本固定显示在最后）

第一版禁止：

- 图片和普通文件同时出现在一条消息里
- 多段富文本 block
- 任意位置穿插图文排版

---

## 6. 消息内容模型

请将消息内容建模为固定结构：

```text
images[]
files[]
text
```

规则：

- `text` 最多一段
- `text` 固定显示在最后
- `images` 可多张
- `files` 可多个
- `images` 和 `files` 不能同时非空
- 空消息无效

消息类型建议：

- `text`
- `image`
- `file`
- `system`

说明：

- `text`：只有文字
- `image`：多图，可附带末尾文本
- `file`：文件，可附带末尾文本
- `system`：系统消息

---

## 7. 附件与去重存储

### 7.1 哈希规则

所有二进制内容（图片、文件）都必须做：

- `SHA-256`

### 7.2 去重规则

按二进制内容哈希去重：

- 若相同 `sha256` 已存在，则不要重复保存物理文件
- 消息只引用已有 blob/media 记录

### 7.3 存储要求

请实现两层存储模型：

#### 7.3.1 blobs

表示物理内容对象。

建议字段：

- `blob_id`
- `sha256`
- `size`
- `mime_type`
- `storage_path`
- `ref_count`
- `created_at`

#### 7.3.2 media_objects

表示可被消息引用的媒体对象。

建议字段：

- `media_id`
- `blob_id`
- `kind`：`image | file`
- `original_name`
- `mime_type`
- `size`
- `width`
- `height`
- `created_by`
- `created_at`

#### 7.3.3 message_attachments

表示消息和媒体的关联。

建议字段：

- `message_id`
- `media_id`
- `sort_order`
- `created_at`

### 7.4 服务端校验要求

即使客户端已计算哈希，服务端也必须重新计算一次 SHA-256，不能只信任客户端传入值。

---

## 8. 协议要求

### 8.1 libp2p 协议名前缀

为了避免与客户端内部协议冲突，服务端协议名前缀统一使用：

```text
/meshspace/session/1.0.0
```

第一版只实现一个主协议即可：

```text
/meshspace/session/1.0.0
```

后续可预留：

```text
/meshspace/media/1.0.0
/meshspace/discovery/1.0.0
```

### 8.2 传输方式

- 使用 libp2p stream
- 使用 length-prefixed 的 protobuf 二进制帧
- 不要用 JSON 作为主协议编码

### 8.3 认证方式

使用 libp2p Peer ID challenge-response：

1. 客户端连接 Node
2. 客户端发 `HELLO`
3. 服务端返回 `AUTH_CHALLENGE`
4. 客户端用 Peer ID 对应私钥签名 challenge
5. 客户端发 `AUTH_PROVE`
6. 服务端校验签名
7. 服务端返回 `AUTH_RESULT`

认证成功后，建立 session。

### 8.4 签名内容要求

签名原文建议包含：

- protocol id
- client peer id
- space peer id
- nonce
- issued_at
- expires_at

要求：

- 防重放
- 有过期时间
- nonce 只能使用一次

---

## 9. ACK / seq 机制

### 9.1 seq 维度

消息序号按 **channel 维度递增**。

不要使用全局 seq。

例如：

- `channel A`: 1, 2, 3, 4
- `channel B`: 1, 2, 3, 4

### 9.2 client_msg_id

客户端发送消息时必须带：

- `client_msg_id`

服务端必须对：

- `(channel_id, sender_user_id, client_msg_id)`

做幂等去重。

### 9.3 send ack

服务端成功入库后返回：

- `client_msg_id`
- `message_id`
- `channel_id`
- `seq`
- `space_time`

### 9.4 deliver ack

客户端收到消息后可以累计回传：

- `channel_id`
- `acked_seq`

### 9.5 read update

客户端阅读到某条消息后更新：

- `channel_id`
- `last_read_seq`

### 9.6 断线重连

客户端重连后应支持：

- 按 `after_seq` 拉取缺失消息
- 重新订阅 channel
- 增量同步，不要全量重推

---

## 10. 协议消息类型

请至少实现以下消息类型：

- `HELLO`
- `AUTH_CHALLENGE`
- `AUTH_PROVE`
- `AUTH_RESULT`
- `PING`
- `PONG`
- `ERROR`
- `LIST_spaceS_REQ`
- `LIST_spaceS_RESP`
- `LIST_CHANNELS_REQ`
- `LIST_CHANNELS_RESP`
- `SUBSCRIBE_CHANNEL_REQ`
- `SUBSCRIBE_CHANNEL_RESP`
- `UNSUBSCRIBE_CHANNEL_REQ`
- `UNSUBSCRIBE_CHANNEL_RESP`
- `SEND_MESSAGE_REQ`
- `SEND_MESSAGE_ACK`
- `MESSAGE_EVENT`
- `CHANNEL_DELIVER_ACK`
- `CHANNEL_READ_UPDATE`
- `SYNC_CHANNEL_REQ`
- `SYNC_CHANNEL_RESP`

请生成 `.proto` 文件，并自动生成 Go 代码。

---

## 11. 权限模型

### 11.1 space 角色

第一版至少支持：

- `owner`
- `admin`
- `member`
- `subscriber`（仅用于只读场景也可以统一简化）

### 11.2 group 权限

群聊中成员可以按权限拥有：

- `view_channel`
- `send_message`
- `send_image`
- `send_file`
- `delete_message`
- `manage_channel`

### 11.3 channel 权限

广播频道默认规则：

- owner/admin：可发消息
- 普通成员：只能看，不能发

### 11.4 可见性

- `public`
- `private`

说明：

- `public`：space 成员可见
- `private`：只有被授权成员可见

---

## 12. 数据库要求

使用 **MySQL 8**。

请提供迁移文件。

至少设计以下表：

- `users`
- `nodes`
- `spaces`
- `space_members`
- `channels`
- `channel_members`
- `messages`
- `user_channel_reads`
- `blobs`
- `media_objects`
- `message_attachments`
- `invites`（可选但建议预留）

要求：

- 主键建议使用 bigint 自增 + 外部业务 ID 双轨模式
- 外部业务 ID 可使用字符串，例如：
  - `u_xxx`
  - `srv_xxx`
  - `ch_xxx`
  - `msg_xxx`
  - `blob_xxx`
  - `media_xxx`
- 关键唯一约束必须加上
- `messages` 表必须支持 `channel_id + seq` 唯一

---

## 13. 配置系统

请实现配置加载模块，优先级建议如下：

1. 环境变量
2. 配置文件
3. 默认值

至少支持配置：

- 服务监听地址
- libp2p 监听地址
- libp2p 私钥路径
- MySQL DSN
- blob 存储目录
- 日志目录
- 协议版本
- 心跳间隔
- 鉴权 challenge 过期时间
- 单条消息最大文本长度
- 单条消息最大图片数
- 单条消息最大文件数
- 单文件最大大小

---

## 14. Docker 要求

### 14.1 Dockerfile

请编写多阶段构建 Dockerfile：

- builder 使用 `golang:1.26`
- runtime 使用尽量小的基础镜像
- 最终容器中包含：
  - meshspace 可执行文件
  - 默认配置目录

### 14.2 docker-compose 目录要求

必须生成：

```text
docker-compose/docker-compose.yml
```

并确保在 `docker-compose/` 目录下执行：

```bash
docker compose up -d --build
```

即可启动所有依赖。

### 14.3 Compose 服务建议

至少包含：

#### 1. meshspace

负责：

- libp2p 服务
- 主业务逻辑
- 消息处理
- 数据库存储

#### 2. mysql

负责：

- 持久化元数据

如无必要，不要加入太多额外组件。

### 14.4 挂载要求

所有数据都必须挂载到 `docker-compose/data/` 下。

示例约束：

- MySQL 数据目录 → `./data/mysql`
- Blob 文件目录 → `./data/blobs`
- 日志目录 → `./data/logs`
- 配置目录 → `./data/config`

### 14.5 推荐端口

请在 README 中写清楚默认端口。可参考：

- libp2p TCP：`4001`
- HTTP 管理/健康检查：`8080`
- MySQL：`3306`

---

## 15. 健康检查与管理接口

虽然主业务使用 libp2p，但请额外提供一个轻量 HTTP 管理接口，至少包括：

- `GET /healthz`
- `GET /readyz`
- `GET /version`

可选：

- `GET /metrics`（若集成 Prometheus）

用途：

- 容器健康检查
- 编排系统探针
- 调试与运维

---

## 16. 日志要求

请实现结构化日志。

要求：

- 支持输出到 stdout
- 支持同时写入 `docker-compose/data/logs/`
- 关键操作必须记录：
  - 节点启动
  - 连接建立
  - 认证成功/失败
  - 订阅 channel
  - 发消息
  - 存储 blob
  - blob 去重命中
  - 数据库错误

---

## 17. 安全与校验要求

### 17.1 鉴权

- 所有业务请求都必须在认证成功后才能执行
- challenge 必须带过期时间
- nonce 不可复用

### 17.2 消息校验

- 文本长度限制
- 图片数量限制
- 文件数量限制
- 图片和文件不能同时非空
- 空消息无效

### 17.3 文件校验

- 服务端必须重新计算 SHA-256
- MIME 类型要做基本校验
- 文件大小要限制

### 17.4 权限校验

- 订阅 channel 前必须校验可见性和成员权限
- 发送消息前必须校验 channel 类型和用户角色
- `channel` 类型中普通成员禁止发送

---

## 18. 启动要求

请确保以下流程可行：

### 18.1 本地一键启动

进入目录：

```bash
cd docker-compose
```

执行：

```bash
docker compose up -d --build
```

即可启动：

- meshspace
- mysql

### 18.2 首次启动

要求自动完成：

- 数据库连接
- 自动迁移或初始化表结构
- 必要目录创建
- 默认配置加载

### 18.3 数据可持久化

删除容器后，只要 `docker-compose/data/` 还在，数据必须保留。

---

## 19. README 要求

请生成清晰的 `README.md`，至少包含：

- 项目简介
- 技术栈
- 目录结构说明
- 快速启动说明
- 环境变量说明
- `docker-compose/` 目录说明
- 数据目录说明
- 端口说明
- libp2p 协议名说明
- 开发模式运行说明
- 常见问题说明

---

## 20. 编码风格要求

- 使用清晰的包结构
- 避免把所有逻辑塞进 `main.go`
- handler、service、repository 分层清楚
- 错误处理明确
- 不要吞掉错误
- 重要结构体和方法要有注释
- 保持实现简洁、可靠、易读

---

## 21. 推荐实现顺序

请按以下优先级实现：

### 第一阶段

- 配置系统
- MySQL 连接
- migrations
- libp2p host 启动
- `/meshspace/session/1.0.0` 协议基础框架
- challenge-response 认证
- space / Channel 列表接口

### 第二阶段

- 订阅 channel
- 发送消息
- `ACK / seq`
- `SYNC_CHANNEL_REQ / RESP`

### 第三阶段

- blob 存储
- SHA-256 去重
- 附件与消息关联
- 权限完善
- HTTP 健康检查
- Docker Compose 和 README 打磨

---

## 22. 最终验收标准

生成的项目必须满足：

1. `docker-compose/docker-compose.yml` 存在
2. 在 `docker-compose/` 目录执行 `docker compose up -d --build` 可以启动
3. MySQL 数据挂载到 `docker-compose/data/mysql/`
4. blob 数据挂载到 `docker-compose/data/blobs/`
5. 日志写到 `docker-compose/data/logs/`
6. Go 版本为 1.26
7. libp2p 主协议为 `/meshspace/session/1.0.0`
8. 支持基于 Peer ID 的 challenge-response 认证
9. 支持 `group` 和 `channel` 两种 channel 类型
10. 支持按 channel 递增的 `seq`
11. 支持 `SEND_MESSAGE_ACK`
12. 支持按 `after_seq` 增量同步
13. 支持附件 SHA-256 去重
14. 支持一条消息的“多图 + 文本”或“文件 + 文本”结构
15. 不支持复杂图文混排

---

## 23. 额外要求

请在生成代码时顺带提供：

- `.env.example`
- `docker-compose/.env.example`
- 初始化配置文件模板
- SQL 迁移文件
- protobuf 文件与生成指令
- 如果有必要，可加 `Makefile`

如果需要在开发体验上补充内容，可以增加：

- `make proto`
- `make build`
- `make run`
- `make migrate`
- `make compose-up`
- `make compose-down`

但不要引入过重的工程复杂度。

---

## 24. 给 Codex 的最终指令

请根据以上要求，直接生成一个完整可运行的 Go 项目，不要只输出示意代码，不要只输出接口设计。必须生成：

- 完整目录结构
- 完整源码
- Dockerfile
- `docker-compose/docker-compose.yml`
- README
- migrations
- `.proto`
- 配置样例

目标是：我拿到代码后，进入 `docker-compose/` 目录执行 `docker compose up -d --build`，就可以直接启动整个系统，并且所有数据都存放在 `docker-compose/data/` 目录内。
## 12. 更细的 MySQL 表结构（必须实现）

要求：

- 使用 **MySQL 8.4+**。
- 所有表使用 `utf8mb4`。
- 所有时间统一使用 `DATETIME(3)` 或 `TIMESTAMP(3)`，建议使用 `DATETIME(3)`。
- 所有主键使用 `BIGINT UNSIGNED AUTO_INCREMENT`。
- 业务外部 ID 使用字符串字段，例如 `user_id`, `space_id`, `channel_id`, `message_id`，格式可为 `u_xxx` / `s_xxx` / `c_xxx` / `m_xxx`。
- 所有 `created_at` / `updated_at` 字段必须由服务端维护。
- 所有外键如果实现困难，可先只建索引不建 FK，但业务逻辑必须保证一致性。

### 12.1 users

```sql
CREATE TABLE users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id VARCHAR(64) NOT NULL,
  peer_id VARCHAR(128) NOT NULL,
  pubkey VARBINARY(255) NULL,
  display_name VARCHAR(100) NOT NULL,
  avatar_url VARCHAR(255) NULL,
  bio VARCHAR(255) NULL,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  last_login_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_user_id (user_id),
  UNIQUE KEY uk_users_peer_id (peer_id),
  KEY idx_users_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

说明：

- `peer_id` 与 `user_id` 一一映射。
- `pubkey` 可选保存，便于审计与后续验证。

### 12.2 nodes

```sql
CREATE TABLE nodes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(64) NOT NULL,
  peer_id VARCHAR(128) NOT NULL,
  name VARCHAR(100) NOT NULL,
  public_addrs JSON NULL,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_nodes_node_id (node_id),
  UNIQUE KEY uk_nodes_peer_id (peer_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

说明：

- 记录运行中的 meshspace 节点。
- 第一版只有一个节点也要保留此表，为后续多节点托管预留。

### 12.3 spaces

```sql
CREATE TABLE spaces (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  space_id VARCHAR(64) NOT NULL,
  host_node_id BIGINT UNSIGNED NOT NULL,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(100) NOT NULL,
  avatar_url VARCHAR(255) NULL,
  description VARCHAR(500) NULL,
  visibility ENUM('public','private') NOT NULL DEFAULT 'private',
  member_count INT UNSIGNED NOT NULL DEFAULT 0,
  channel_count INT UNSIGNED NOT NULL DEFAULT 0,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_spaces_space_id (space_id),
  KEY idx_spaces_owner_user_id (owner_user_id),
  KEY idx_spaces_host_node_id (host_node_id),
  KEY idx_spaces_visibility (visibility)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 12.4 space_members

```sql
CREATE TABLE space_members (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  space_id BIGINT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  role ENUM('owner','admin','member','subscriber') NOT NULL DEFAULT 'member',
  nickname VARCHAR(100) NULL,
  is_muted TINYINT UNSIGNED NOT NULL DEFAULT 0,
  is_banned TINYINT UNSIGNED NOT NULL DEFAULT 0,
  joined_at DATETIME(3) NOT NULL,
  last_seen_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_space_members_space_user (space_id, user_id),
  KEY idx_space_members_user_id (user_id),
  KEY idx_space_members_space_role (space_id, role),
  KEY idx_space_members_space_banned (space_id, is_banned)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 12.5 channels

```sql
CREATE TABLE channels (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  channel_id VARCHAR(64) NOT NULL,
  space_id BIGINT UNSIGNED NOT NULL,
  type ENUM('group','channel') NOT NULL,
  name VARCHAR(100) NOT NULL,
  description VARCHAR(255) NULL,
  visibility ENUM('public','private') NOT NULL DEFAULT 'public',
  slow_mode_seconds INT UNSIGNED NOT NULL DEFAULT 0,
  message_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  message_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
  subscriber_count INT UNSIGNED NOT NULL DEFAULT 0,
  created_by BIGINT UNSIGNED NOT NULL,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_channels_channel_id (channel_id),
  KEY idx_channels_space_id (space_id),
  KEY idx_channels_space_sort (space_id, sort_order),
  KEY idx_channels_space_type (space_id, type),
  KEY idx_channels_visibility (visibility)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

说明：

- `message_seq` 保存该 channel 当前最新 seq。
- `channel.type='channel'` 表示广播频道。

### 12.6 channel_members

```sql
CREATE TABLE channel_members (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  channel_id BIGINT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  role ENUM('owner','admin','member','subscriber') NOT NULL DEFAULT 'member',
  can_view TINYINT UNSIGNED NOT NULL DEFAULT 1,
  can_send_message TINYINT UNSIGNED NOT NULL DEFAULT 1,
  can_send_image TINYINT UNSIGNED NOT NULL DEFAULT 1,
  can_send_file TINYINT UNSIGNED NOT NULL DEFAULT 1,
  can_delete_message TINYINT UNSIGNED NOT NULL DEFAULT 0,
  can_manage_channel TINYINT UNSIGNED NOT NULL DEFAULT 0,
  joined_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_channel_members_channel_user (channel_id, user_id),
  KEY idx_channel_members_user_id (user_id),
  KEY idx_channel_members_channel_role (channel_id, role)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

规则：

- 对 `type='group'`，普通成员默认 `can_send_message=1`。
- 对 `type='channel'`，普通订阅者默认 `can_send_message=0`、`can_send_image=0`、`can_send_file=0`。
- 可在服务端创建频道时根据类型自动写入默认权限。

### 12.7 messages

```sql
CREATE TABLE messages (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  message_id VARCHAR(64) NOT NULL,
  channel_id BIGINT UNSIGNED NOT NULL,
  seq BIGINT UNSIGNED NOT NULL,
  sender_user_id BIGINT UNSIGNED NOT NULL,
  client_msg_id VARCHAR(64) NOT NULL,
  message_type ENUM('text','image','file','system') NOT NULL,
  text_content TEXT NULL,
  status ENUM('normal','deleted') NOT NULL DEFAULT 'normal',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  deleted_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_messages_message_id (message_id),
  UNIQUE KEY uk_messages_channel_seq (channel_id, seq),
  UNIQUE KEY uk_messages_channel_sender_clientmsg (channel_id, sender_user_id, client_msg_id),
  KEY idx_messages_channel_created (channel_id, created_at),
  KEY idx_messages_sender_user_id (sender_user_id),
  KEY idx_messages_channel_status_seq (channel_id, status, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

规则：

- `text_content` 可为空。
- `message_type='text'` 时，必须有非空 `text_content` 且无附件。
- `message_type='image'` 时，必须有图片附件，可带末尾文本。
- `message_type='file'` 时，必须有文件附件，可带末尾文本。
- `message_type='system'` 仅系统内部生成。

### 12.8 user_channel_reads

```sql
CREATE TABLE user_channel_reads (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  channel_id BIGINT UNSIGNED NOT NULL,
  last_delivered_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  last_read_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_user_channel_reads_user_channel (user_id, channel_id),
  KEY idx_user_channel_reads_channel_id (channel_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 12.9 blobs

```sql
CREATE TABLE blobs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  blob_id VARCHAR(80) NOT NULL,
  sha256 CHAR(64) NOT NULL,
  size BIGINT UNSIGNED NOT NULL,
  mime_type VARCHAR(128) NULL,
  storage_path VARCHAR(255) NOT NULL,
  ref_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_blobs_blob_id (blob_id),
  UNIQUE KEY uk_blobs_sha256 (sha256),
  KEY idx_blobs_size (size)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 12.10 media_objects

```sql
CREATE TABLE media_objects (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  media_id VARCHAR(80) NOT NULL,
  blob_id BIGINT UNSIGNED NOT NULL,
  kind ENUM('image','file') NOT NULL,
  original_name VARCHAR(255) NULL,
  mime_type VARCHAR(128) NULL,
  size BIGINT UNSIGNED NOT NULL,
  width INT UNSIGNED NULL,
  height INT UNSIGNED NULL,
  created_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_media_objects_media_id (media_id),
  KEY idx_media_objects_blob_id (blob_id),
  KEY idx_media_objects_kind (kind),
  KEY idx_media_objects_created_by (created_by)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

说明：

- 同一个 `blob_id` 可以有多个 `media_object`，但第一版通常一个 blob 对应一个 media 即可。
- 图片保存 `width` / `height`，普通文件可为空。

### 12.11 message_attachments

```sql
CREATE TABLE message_attachments (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  message_id BIGINT UNSIGNED NOT NULL,
  media_id BIGINT UNSIGNED NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_message_attachments_message_media (message_id, media_id),
  KEY idx_message_attachments_message_id (message_id),
  KEY idx_message_attachments_media_id (media_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 12.12 auth_nonces

```sql
CREATE TABLE auth_nonces (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  nonce_hash CHAR(64) NOT NULL,
  client_peer_id VARCHAR(128) NOT NULL,
  space_peer_id VARCHAR(128) NOT NULL,
  issued_at DATETIME(3) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  used_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_auth_nonces_nonce_hash (nonce_hash),
  KEY idx_auth_nonces_client_peer_id (client_peer_id),
  KEY idx_auth_nonces_expires_at (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

用于 challenge-response 防重放。

### 12.13 space_invites（可选但建议实现）

```sql
CREATE TABLE space_invites (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  invite_code VARCHAR(64) NOT NULL,
  space_id BIGINT UNSIGNED NOT NULL,
  created_by BIGINT UNSIGNED NOT NULL,
  max_uses INT UNSIGNED NULL,
  used_count INT UNSIGNED NOT NULL DEFAULT 0,
  expires_at DATETIME(3) NULL,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_space_invites_invite_code (invite_code),
  KEY idx_space_invites_space_id (space_id),
  KEY idx_space_invites_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

## 13. 数据库约束与业务规则（必须在代码中实现）

### 13.1 发送消息约束

服务端在处理 `SEND_MESSAGE_REQ` 时必须校验：

1. `channel_id` 存在。
2. 当前用户已加入对应 space。
3. 当前用户有该 Channel 的查看权限。
4. 对 `group`：用户需有发送权限。
5. 对 `channel`：仅 `owner/admin` 或显式允许者可发送。
6. `client_msg_id` 不能为空，长度建议 8~64。
7. 空消息禁止。
8. 图片和文件不能同时出现在一条消息内。
9. 文本长度建议限制为 4000 字符以内。
10. 图片数量建议限制为 9 以内。
11. 文件数量建议限制为 5 以内。
12. 若配置了 `slow_mode_seconds`，需要检查同一用户在该 channel 最近一次发言时间。

### 13.2 附件约束

1. 客户端可以先算 `sha256`，但服务端必须重算并验证。
2. 若 `sha256` 已存在于 `blobs`，不得重复写物理文件。
3. `storage_path` 应按哈希分层，例如：

```text
./data/blobs/ab/cd/<sha256>
```

4. `ref_count` 应在消息创建/删除时正确维护。
5. 删除消息时，第一版可以只做逻辑删除，不立即删除 blob；可后续通过 GC 清理 `ref_count=0` 且过期对象。

### 13.3 seq 规则

1. 每个 Channel 独立维护 `message_seq`。
2. 新消息写入前，在事务内获取并递增 `channels.message_seq`。
3. `messages.seq` 必须严格单调递增，且在同一 `channel_id` 内唯一。
4. 重连补消息时，统一按 `after_seq` 查询。

## 14. 更细的 proto 字段（必须实现）

请创建文件：

```text
proto/meshspace/session/v1/session.proto
```

内容至少应包含下面这些定义；可以在不破坏含义的前提下做微调，但字段含义不要改变。

```proto
syntax = "proto3";

package meshspace.session.v1;

option go_package = "meshspace/proto/meshspace/session/v1;sessionv1";

message Envelope {
  uint32 version = 1;
  MsgType msg_type = 2;
  string request_id = 3;
  uint64 timestamp_ms = 4;
  bytes body = 5;
}

enum MsgType {
  MSG_TYPE_UNSPECIFIED = 0;

  HELLO = 1;
  AUTH_CHALLENGE = 2;
  AUTH_PROVE = 3;
  AUTH_RESULT = 4;

  PING = 10;
  PONG = 11;
  ERROR = 12;

  LIST_spaceS_REQ = 20;
  LIST_spaceS_RESP = 21;

  LIST_CHANNELS_REQ = 22;
  LIST_CHANNELS_RESP = 23;

  SUBSCRIBE_CHANNEL_REQ = 24;
  SUBSCRIBE_CHANNEL_RESP = 25;
  UNSUBSCRIBE_CHANNEL_REQ = 26;
  UNSUBSCRIBE_CHANNEL_RESP = 27;

  SEND_MESSAGE_REQ = 30;
  SEND_MESSAGE_ACK = 31;
  MESSAGE_EVENT = 32;

  CHANNEL_DELIVER_ACK = 33;
  CHANNEL_READ_UPDATE = 34;

  SYNC_CHANNEL_REQ = 35;
  SYNC_CHANNEL_RESP = 36;
}

enum Visibility {
  VISIBILITY_UNSPECIFIED = 0;
  PUBLIC = 1;
  PRIVATE = 2;
}

enum ChannelType {
  CHANNEL_TYPE_UNSPECIFIED = 0;
  GROUP = 1;
  BROADCAST = 2;
}

enum MessageType {
  MESSAGE_TYPE_UNSPECIFIED = 0;
  TEXT = 1;
  IMAGE = 2;
  FILE = 3;
  SYSTEM = 4;
}

enum MemberRole {
  MEMBER_ROLE_UNSPECIFIED = 0;
  OWNER = 1;
  ADMIN = 2;
  MEMBER = 3;
  SUBSCRIBER = 4;
}

message Hello {
  string client_peer_id = 1;
  string client_agent = 2;
  string protocol_version = 3;
}

message AuthChallenge {
  string space_peer_id = 1;
  bytes nonce = 2;
  uint64 issued_at_ms = 3;
  uint64 expires_at_ms = 4;
  string session_hint = 5;
}

message AuthProve {
  string client_peer_id = 1;
  bytes nonce = 2;
  uint64 issued_at_ms = 3;
  uint64 expires_at_ms = 4;
  bytes signature = 5;
  bytes public_key = 6;
}

message AuthResult {
  bool ok = 1;
  string session_id = 2;
  string user_id = 3;
  string display_name = 4;
  string message = 5;
  repeated spaceSummary spaces = 6;
}

message ErrorMsg {
  uint32 code = 1;
  string message = 2;
}

message Ping {
  uint64 nonce = 1;
}

message Pong {
  uint64 nonce = 1;
}

message spaceSummary {
  string space_id = 1;
  string name = 2;
  string avatar_url = 3;
  string description = 4;
  Visibility visibility = 5;
  uint32 member_count = 6;
}

message ListspacesReq {}

message ListspacesResp {
  repeated spaceSummary spaces = 1;
}

message ChannelSummary {
  string channel_id = 1;
  string space_id = 2;
  ChannelType type = 3;
  string name = 4;
  string description = 5;
  Visibility visibility = 6;
  uint32 slow_mode_seconds = 7;
  uint64 last_seq = 8;
  bool can_view = 9;
  bool can_send_message = 10;
  bool can_send_image = 11;
  bool can_send_file = 12;
}

message ListChannelsReq {
  string space_id = 1;
}

message ListChannelsResp {
  string space_id = 1;
  repeated ChannelSummary channels = 2;
}

message SubscribeChannelReq {
  string channel_id = 1;
  uint64 last_seen_seq = 2;
}

message SubscribeChannelResp {
  bool ok = 1;
  string channel_id = 2;
  uint64 current_last_seq = 3;
  string message = 4;
}

message UnsubscribeChannelReq {
  string channel_id = 1;
}

message UnsubscribeChannelResp {
  bool ok = 1;
  string channel_id = 2;
}

message MediaImage {
  string media_id = 1;
  string blob_id = 2;
  string sha256 = 3;
  string url = 4;
  uint32 width = 5;
  uint32 height = 6;
  string mime_type = 7;
  uint64 size = 8;
}

message MediaFile {
  string media_id = 1;
  string blob_id = 2;
  string sha256 = 3;
  string file_name = 4;
  string url = 5;
  string mime_type = 6;
  uint64 size = 7;
}

message MessageContent {
  repeated MediaImage images = 1;
  repeated MediaFile files = 2;
  string text = 3;
}

message SendMessageReq {
  string channel_id = 1;
  string client_msg_id = 2;
  MessageType message_type = 3;
  MessageContent content = 4;
}

message SendMessageAck {
  bool ok = 1;
  string channel_id = 2;
  string client_msg_id = 3;
  string message_id = 4;
  uint64 seq = 5;
  uint64 space_time_ms = 6;
  string message = 7;
}

message MessageEvent {
  string channel_id = 1;
  string message_id = 2;
  uint64 seq = 3;
  string sender_user_id = 4;
  MessageType message_type = 5;
  MessageContent content = 6;
  uint64 created_at_ms = 7;
}

message ChannelDeliverAck {
  string channel_id = 1;
  uint64 acked_seq = 2;
}

message ChannelReadUpdate {
  string channel_id = 1;
  uint64 last_read_seq = 2;
}

message SyncChannelReq {
  string channel_id = 1;
  uint64 after_seq = 2;
  uint32 limit = 3;
}

message SyncChannelResp {
  string channel_id = 1;
  repeated MessageEvent messages = 2;
  uint64 next_after_seq = 3;
  bool has_more = 4;
}
```

## 15. proto 编码与 Go 集成要求

1. 使用 `protoc` 生成 Go 代码。
2. 需要提供生成脚本，例如：

```text
scripts/gen-proto.sh
```

3. Docker 构建时应确保 proto 已生成，或者在构建阶段自动生成。
4. `README.md` 中必须说明如何重新生成 proto。

## 16. 建议的迁移文件拆分

请在 `migrations/` 中至少提供这些文件：

```text
0001_init_users_and_nodes.sql
0002_spaces_and_members.sql
0003_channels.sql
0004_messages.sql
0005_blobs_and_media.sql
0006_reads_and_auth_nonces.sql
0007_invites.sql
```

要求：

- 同时提供 up/down，或者使用你选择的迁移工具约定格式。
- 应支持项目首次启动自动执行迁移（推荐）。

## 17. Docker Compose 细化要求

### 17.1 docker-compose/docker-compose.yml

必须至少包含：

- `meshspace`
- `mysql`

可选：

- `adminer` 或 `phpmyadmin`（默认可关闭）

### 17.2 数据目录映射

必须映射：

```text
./data/mysql      -> /var/lib/mysql
./data/blobs      -> /app/data/blobs
./data/logs       -> /app/data/logs
./data/config     -> /app/data/config
```

### 17.3 端口建议

可以使用默认示例：

- meshspace libp2p: `4001`
- meshspace http admin/health: `8080`
- mysql: `3306`

### 17.4 首次启动要求

在 `docker-compose/` 目录执行：

```bash
docker compose up -d --build
```

后应自动完成：

1. MySQL 启动
2. meshspace 等待数据库可用
3. 自动建表 / 跑迁移
4. 自动生成或加载节点身份密钥
5. 开始监听 libp2p 协议 `/meshspace/session/1.0.0`

## 18. Codex 输出代码时必须满足的关键点

1. 代码必须能实际编译通过。
2. 不要只输出伪代码。
3. 所有 import 必须正确。
4. 所有未实现函数必须补齐，不允许大量 TODO 占位。
5. 所有数据库操作都要有真实 SQL 或 ORM 映射。
6. 所有 libp2p 协议处理函数都要有最小可运行实现。
7. 至少实现：
   - 认证
   - 列出 space
   - 列出 Channel
   - 订阅 Channel
   - 发送消息
   - ACK / seq 同步
   - blob SHA-256 去重存储
8. 必须实现健康检查接口，例如：
   - `GET /healthz`
   - `GET /readyz`
9. 必须实现配置加载与默认值。
10. README 必须说明如何启动、如何连接、如何生成 proto、如何查看数据目录。

## 19. 最终目标

请直接生成一个可运行的 Go 项目，而不是仅生成设计草图。

最小可用标准：

- `docker compose up -d --build` 后服务可启动
- libp2p 协议 `/meshspace/session/1.0.0` 已注册
- 客户端可以完成 challenge-response 认证
- 能列出 space / Channel
- 能在 `group` 中发文本消息
- 能在 `channel` 中限制只有管理员发消息
- 能正确写入 MySQL
- 能按 `channel seq` 增量同步
- 能对上传的二进制文件计算 SHA-256 并去重存储

## 20. 为了让 Codex 一次生成成功，必须遵守的实现边界

### 20.1 先做最小可运行版本，不要过度扩展

第一版只要求实现以下闭环：

1. libp2p 节点启动
2. `/meshspace/session/1.0.0` 协议注册
3. challenge-response 认证
4. 列出 space
5. 列出 Channel
6. 订阅 Channel
7. 发送文本消息
8. Channel 维度 ACK / seq
9. 文件 SHA-256 去重存储
10. MySQL 持久化
11. `docker compose up -d --build` 一键启动

第一版不要主动扩展这些复杂特性：

- 多设备身份合并
- 端到端加密消息体
- 反应/reaction
- 撤回/编辑
- 复杂全文搜索
- 频道评论
- 节点间联邦同步
- 后台任务系统
- MQ / Kafka / Redis
- 分布式对象存储

### 20.2 Codex 必须优先保证“能跑起来”

取舍优先级必须是：

1. 可编译
2. 可启动
3. 可连接
4. 可落库
5. 可基本收发消息
6. 再考虑抽象和扩展性

如果某些扩展抽象会明显增加复杂度，应选择更直接的实现。

---

## 21. Codex 生成代码时的目录职责说明（必须遵守）

```text
cmd/
  meshspace/
    main.go                # 程序入口，组装配置、数据库、libp2p、HTTP 管理接口

internal/
  app/                     # 应用启动、关闭、依赖装配
  auth/                    # challenge-response 认证逻辑
  config/                  # 配置结构体、环境变量加载、默认值
  db/                      # MySQL 连接、迁移执行、事务辅助
  libp2p/                  # host 初始化、协议注册、stream handler、节点身份加载
  protocol/                # protobuf envelope 编解码、消息类型分发
  session/                 # 连接会话、认证态、订阅态、会话管理
  space/                  # space 领域模型和 service/repo
  channel/                 # channel 领域模型和 service/repo
  message/                 # message 领域模型和 service/repo
  media/                   # blob/hash/去重/附件元信息
  storage/                 # 本地文件存储实现
  service/                 # 跨领域 service 编排
  repository/              # repository 接口定义（可按领域拆）
  api/                     # HTTP 管理与健康检查接口
  logx/                    # 日志初始化

migrations/                # SQL 迁移文件
proto/                     # proto 源文件
scripts/                   # proto 生成脚本、开发辅助脚本
```

要求：

- `internal/service` 只做编排，不要堆积所有 SQL。
- 数据库访问逻辑应放到各领域 repo 中，或者放在 `internal/repository/mysql/...`。
- `internal/protocol` 不直接依赖 HTTP。
- `internal/libp2p` 不要直接写业务 SQL。

---

## 22. 建议的 Go 包与核心类型（Codex 应直接创建）

### 22.1 config

至少包含：

- `type Config struct`
- `func Load() (*Config, error)`
- `func (c *Config) Validate() error`

建议字段：

- `MySQLDSN string`
- `Libp2pListenAddrs []string`
- `Libp2pProtocolID string`
- `NodeKeyPath string`
- `BlobRoot string`
- `LogDir string`
- `HTTPListenAddr string`
- `ReadTimeout time.Duration`
- `WriteTimeout time.Duration`
- `MaxTextLen int`
- `MaxImagesPerMessage int`
- `MaxFilesPerMessage int`
- `MaxUploadBytes int64`

### 22.2 app

至少包含：

- `type App struct`
- `func New(cfg *config.Config) (*App, error)`
- `func (a *App) Start(ctx context.Context) error`
- `func (a *App) Shutdown(ctx context.Context) error`

### 22.3 libp2p

至少包含：

- `type Node struct`
- `func NewNode(cfg *config.Config, sessionHandler network.StreamHandler) (*Node, error)`
- `func (n *Node) Start() error`
- `func (n *Node) Close() error`
- `func LoadOrCreateIdentity(path string) (crypto.PrivKey, error)`

### 22.4 protocol

至少包含：

- `type EnvelopeCodec struct`
- `func ReadEnvelope(r io.Reader) (*sessionv1.Envelope, error)`
- `func WriteEnvelope(w io.Writer, env *sessionv1.Envelope) error`
- `func MarshalBody(msg proto.Message) ([]byte, error)`
- `func UnmarshalBody(data []byte, msg proto.Message) error`

### 22.5 session

至少包含：

- `type Manager struct`
- `type ConnSession struct`
- `func NewManager(...) *Manager`
- `func (m *Manager) HandleStream(s network.Stream)`
- `func (m *Manager) Authenticate(...)`
- `func (m *Manager) SubscribeChannel(...)`
- `func (m *Manager) DeliverMessage(...)`

### 22.6 media / storage

至少包含：

- `type BlobService struct`
- `func (s *BlobService) Put(ctx context.Context, r io.Reader, meta PutBlobInput) (*PutBlobResult, error)`
- `func (s *BlobService) StatBySHA256(ctx context.Context, sha256 string) (*Blob, error)`
- `func (s *BlobService) Open(path string) (io.ReadCloser, error)`

### 22.7 api

至少包含：

- `func NewHTTPspace(cfg *config.Config, deps ...) *http.space`
- `GET /healthz`
- `GET /readyz`
- `GET /debug/config`（可选，默认关闭或仅开发环境）

---

## 23. 推荐的 repository 接口（Codex 应先定义接口，再给 MySQL 实现）

### 23.1 UserRepository

```go
 type UserRepository interface {
     GetByPeerID(ctx context.Context, peerID string) (*User, error)
     CreateIfNotExistsByPeerID(ctx context.Context, peerID string) (*User, error)
 }
```

### 23.2 spaceRepository

```go
 type spaceRepository interface {
     ListByUserID(ctx context.Context, userID int64) ([]*space, error)
     GetByspaceID(ctx context.Context, spaceID string) (*space, error)
 }
```

### 23.3 ChannelRepository

```go
 type ChannelRepository interface {
     ListByspaceIDForUser(ctx context.Context, spaceID string, userID int64) ([]*Channel, error)
     GetByChannelID(ctx context.Context, channelID string) (*Channel, error)
     IsUserMember(ctx context.Context, channelID string, userID int64) (bool, error)
     GetMemberRole(ctx context.Context, channelID string, userID int64) (string, error)
 }
```

### 23.4 MessageRepository

```go
 type MessageRepository interface {
     Create(ctx context.Context, in CreateMessageInput) (*Message, error)
     ListAfterSeq(ctx context.Context, channelID string, afterSeq uint64, limit uint32) ([]*Message, error)
     GetByClientMsgID(ctx context.Context, channelID string, senderUserID int64, clientMsgID string) (*Message, error)
 }
```

### 23.5 ReadCursorRepository

```go
 type ReadCursorRepository interface {
     UpsertDeliveredSeq(ctx context.Context, userID int64, channelID string, seq uint64) error
     UpsertReadSeq(ctx context.Context, userID int64, channelID string, seq uint64) error
     GetByUserAndChannel(ctx context.Context, userID int64, channelID string) (*UserChannelRead, error)
 }
```

### 23.6 BlobRepository

```go
 type BlobRepository interface {
     GetBySHA256(ctx context.Context, sha256 string) (*Blob, error)
     Create(ctx context.Context, in CreateBlobInput) (*Blob, error)
     IncRef(ctx context.Context, blobID string) error
 }
```

### 23.7 MediaRepository

```go
 type MediaRepository interface {
     Create(ctx context.Context, in CreateMediaInput) (*MediaObject, error)
     GetByMediaID(ctx context.Context, mediaID string) (*MediaObject, error)
 }
```

---

## 24. 建议的 service 接口（Codex 直接实现最小版本）

### 24.1 AuthService

```go
 type AuthService interface {
     IssueChallenge(ctx context.Context, clientPeerID string, spacePeerID string) (*Challenge, error)
     VerifyChallenge(ctx context.Context, in VerifyChallengeInput) (*AuthResult, error)
 }
```

### 24.2 DirectoryService

```go
 type DirectoryService interface {
     Listspaces(ctx context.Context, userID int64) ([]*space, error)
     ListChannels(ctx context.Context, userID int64, spaceID string) ([]*Channel, error)
 }
```

### 24.3 MessagingService

```go
 type MessagingService interface {
     SendMessage(ctx context.Context, userID int64, in SendMessageInput) (*Message, error)
     SyncChannel(ctx context.Context, userID int64, channelID string, afterSeq uint64, limit uint32) ([]*Message, uint64, bool, error)
     AckDelivered(ctx context.Context, userID int64, channelID string, seq uint64) error
     UpdateRead(ctx context.Context, userID int64, channelID string, seq uint64) error
 }
```

### 24.4 MediaService

```go
 type MediaService interface {
     SaveUploadedBlob(ctx context.Context, in SaveUploadedBlobInput) (*MediaObject, error)
 }
```

---

## 25. 对 Codex 的实现顺序要求（必须按顺序生成）

请按下面顺序生成代码，不要跳步：

### 第一步：基础骨架

先生成：

- `go.mod`
- `cmd/meshspace/main.go`
- `internal/config`
- `internal/logx`
- `README.md`
- `docker-compose/docker-compose.yml`
- `Dockerfile`
- `.env.example`

### 第二步：数据库与迁移

生成：

- MySQL 初始化连接
- migrations SQL 文件
- 启动时自动跑迁移
- 基础 repository 结构

### 第三步：proto 与编解码

生成：

- `proto/meshspace/session/v1/session.proto`
- `scripts/gen-proto.sh`
- `internal/protocol` 编解码

### 第四步：libp2p node 与 session handler

生成：

- libp2p host 初始化
- identity 加载/生成
- 注册 `/meshspace/session/1.0.0`
- stream handler

### 第五步：认证闭环

生成：

- HELLO
- AUTH_CHALLENGE
- AUTH_PROVE
- AUTH_RESULT

### 第六步：目录查询闭环

生成：

- LIST_spaceS_REQ / RESP
- LIST_CHANNELS_REQ / RESP

### 第七步：消息闭环

生成：

- SUBSCRIBE_CHANNEL_REQ / RESP
- SEND_MESSAGE_REQ / ACK
- MESSAGE_EVENT
- SYNC_CHANNEL_REQ / RESP
- CHANNEL_DELIVER_ACK
- CHANNEL_READ_UPDATE

### 第八步：媒体闭环

生成：

- SHA-256 去重
- blob 本地存储
- media 元数据写库
- 消息引用附件

### 第九步：HTTP 管理面

生成：

- `/healthz`
- `/readyz`
- 基础 metrics/logging 钩子（最小实现即可）

---

## 26. 给 Codex 的 docker-compose.yml 生成要求（必须按这个思路写）

### 26.1 Compose 文件规范

Docker 官方当前推荐使用 Compose Specification；服务定义中的 `build` 是规范支持的内容。citeturn102786search1turn102786search7turn102786search11

因此请直接使用现代 `docker compose` 风格，不要求旧的 `version: '3'` 头部。

### 26.2 必须生成的服务

至少生成：

- `mysql`
- `meshspace`

可选生成：

- `adminer`

### 26.3 Compose 目录内的相对路径要求

从 `docker-compose/docker-compose.yml` 出发，必须保证以下挂载都使用相对路径：

```text
./data/mysql
./data/blobs
./data/logs
./data/config
```

### 26.4 推荐的 Compose 结构

- `mysql` 使用官方 MySQL 8.4 镜像
- `meshspace` 使用项目根目录 `../` 作为 build context
- `meshspace` 依赖 `mysql`
- `meshspace` 启动命令中应等待 MySQL 可用，或程序内部重试连接

### 26.5 Compose 文件里建议暴露的端口

- `4001:4001/tcp`
- `4001:4001/udp`（如果你选择启用 QUIC 或相关传输）
- `8080:8080`
- `3306:3306`

---

## 27. 给 Codex 的 Dockerfile 生成要求

### 27.1 Go 版本要求

Go 官方已发布 Go 1.26（2026-02-10），且 1.26.1 已在 2026-03-05 发布。构建镜像时应使用 Go 1.26 系列镜像。citeturn102786search0turn102786search4

请直接使用：

```dockerfile
FROM golang:1.26 AS builder
```

如果 Codex 想更保守，可以锁到：

```dockerfile
FROM golang:1.26.1 AS builder
```

### 27.2 运行时镜像要求

- 尽量用轻量镜像
- 但必须保证 CA 证书、时区、必要系统库存在
- 若使用 `distroless` 会增加调试复杂度，第一版建议使用 `debian:bookworm-slim` 或类似轻量基础镜像

### 27.3 Dockerfile 必须包含

- 多阶段构建
- 下载依赖缓存优化
- 编译输出到 `/app/meshspace`
- 容器运行目录 `/app`
- 创建 `/app/data/blobs` `/app/data/logs` `/app/data/config`

---

## 28. 给 Codex 的 README 结构要求

README 至少必须包含这些标题：

1. 项目简介
2. 功能范围
3. 目录结构
4. 环境变量
5. 如何生成 proto
6. 如何本地运行
7. 如何使用 Docker Compose 启动
8. 数据目录说明
9. 协议说明
10. 健康检查接口
11. 已实现范围与未实现范围

---

## 29. 给 Codex 的 proto 生成要求

### 29.1 必须提供脚本

生成：

```text
scripts/gen-proto.sh
```

脚本职责：

- 检查 `protoc` 是否存在
- 检查 `protoc-gen-go` 是否存在
- 检查 `protoc-gen-go-grpc` 是否存在（即使暂时不用 gRPC，也可以不强依赖）
- 生成 `.pb.go`

### 29.2 生成位置

建议输出到：

```text
internal/gen/proto/meshspace/session/v1/
```

要求在 README 中明确写出生成命令。

---

## 30. 给 Codex 的最小演示数据要求

为了让项目首次启动后就能验证功能，必须在迁移或 seed 阶段创建最小演示数据：

### 30.1 默认 space

创建一个默认社区，例如：

- `space_id = srv_demo`
- `name = Demo space`

### 30.2 默认 Channel

在该 space 下创建至少两个 Channel：

1. `group`
   - `channel_id = ch_demo_group`
   - `name = General`
   - `type = group`

2. `channel`
   - `channel_id = ch_demo_broadcast`
   - `name = Announcements`
   - `type = channel`

### 30.3 默认成员关系

认证成功的新用户，如果数据库中不存在对应 membership，可自动加入默认 demo space，并加入这两个默认 channel。

这样 Codex 生成的系统在首次连接后就能直接跑通：

- LIST_spaceS
- LIST_CHANNELS
- SUBSCRIBE_CHANNEL
- SEND_MESSAGE（在 group 中）

---

## 31. 给 Codex 的验收脚本建议

Codex 应额外生成一个最小自检脚本，例如：

```text
scripts/check.sh
```

建议内容：

- `go fmt ./...`
- `go test ./...`
- `go vet ./...`
- 检查 `docker-compose/docker-compose.yml` 是否存在
- 检查 `proto/meshspace/session/v1/session.proto` 是否存在
- 检查 `migrations/` 是否非空

可不强制要求单元测试很完整，但结构要在。

---

## 32. 给 Codex 的最终强约束指令

请直接生成完整工程代码，而不是仅给设计说明。

必须满足：

1. 代码可编译。
2. Docker 可构建。
3. Docker Compose 可启动。
4. MySQL 数据会持久化到 `docker-compose/data/mysql/`。
5. Blob 文件会持久化到 `docker-compose/data/blobs/`。
6. 节点身份文件会持久化到 `docker-compose/data/config/`。
7. 运行日志会输出到 stdout，并可选择写入 `docker-compose/data/logs/`。
8. 已实现 `/meshspace/session/1.0.0`。
9. 已实现 challenge-response 认证。
10. 已实现基于 `channel_id + seq` 的增量同步。
11. 已实现 `group` 可发消息、`channel` 普通成员不可发。
12. 已实现 SHA-256 blob 去重。
13. 已提供 README、Dockerfile、docker-compose/docker-compose.yml、proto、migrations、脚本。

