# MeshServer

## 项目简介

`meshserver` 是一个基于 Go 1.26 与 libp2p 的服务端节点，实现了 `/meshserver/session/1.0.0` 会话协议、基于 Peer ID 的 challenge-response 认证、Space / channel 目录查询、频道订阅、消息发送、按 channel 递增的 `seq` 增量同步，以及附件 SHA-256 去重存储。

这里的层级关系按文档语义理解为 `node -> space -> channel`。协议字段里的 `space_id`、`channel_id` 现在都已经改成 `uint32` 自增数值主键。

项目目标是开箱即跑：进入 [docker-compose/docker-compose.yml](/mnt/e/code/meshserver/docker-compose/docker-compose.yml) 所在目录后执行 `docker compose up -d --build` 即可启动服务和 MySQL，并把运行数据全部落在 `docker-compose/data/` 下。

## 功能范围

已实现：

- libp2p 节点启动与身份密钥持久化
- 协议 `/meshserver/session/1.0.0`
- `HELLO -> AUTH_CHALLENGE -> AUTH_PROVE -> AUTH_RESULT` 鉴权闭环
- `LIST_SPACES_REQ / RESP`
- `LIST_CHANNELS_REQ / RESP`
- `SUBSCRIBE_CHANNEL_REQ / RESP`
- `JOIN_SPACE_REQ / RESP`
- `INVITE_SPACE_MEMBER_REQ / RESP`
- `KICK_SPACE_MEMBER_REQ / RESP`
- `BAN_SPACE_MEMBER_REQ / RESP`
- `LIST_SPACE_MEMBERS_REQ / RESP`
- `UNBAN_SPACE_MEMBER_REQ / RESP`
- `CREATE_GROUP_REQ / RESP`
- `CREATE_CHANNEL_REQ / RESP`
- `ADMIN_SET_SPACE_MEMBER_ROLE_REQ / RESP`
- `ADMIN_SET_SPACE_CHANNEL_CREATION_REQ / RESP`
- `SEND_MESSAGE_REQ / ACK`
- `MESSAGE_EVENT`
- `CHANNEL_DELIVER_ACK`
- `CHANNEL_READ_UPDATE`
- `SYNC_CHANNEL_REQ / RESP`
- `GET_MEDIA_REQ / RESP`
- 按 `channel_id + seq` 单调递增的消息序列
- `(channel_id, sender_user_id, client_msg_id)` 幂等去重
- 图片/文件附件 SHA-256 去重存储
- HTTP 管理面：`/healthz`、`/readyz`、`/version`
- 首次启动自动迁移，不再自动创建任何默认 Space 或 channel
- 管理员可以通过 libp2p 会话修改成员角色、邀请/踢出/封禁/解封成员、分页列出成员和创建频道开关

文档里的层级关系按 `node -> space -> channel` 理解。现有协议字段已经统一为 `uint32` 类型的 `space_id`、`channel_id`。

未实现：

- 多设备身份合并
- 节点间联邦同步
- 消息编辑、撤回、reaction
- 复杂全文搜索与后台任务
- 独立的媒体上传协议 `/meshserver/media/1.0.0`

说明：

- 第一版为了让附件闭环可用，在 `MediaImage` / `MediaFile` 上额外加入了 `inline_data` 字段，允许客户端直接通过 `SEND_MESSAGE_REQ` 内联上传二进制内容。
- 现在也支持通过 libp2p 的 `GET_MEDIA_REQ` 直接拉取文件，返回的 `MediaFile.inline_data` 就是文件内容。

## 目录结构

```text
meshserver/
  cmd/meshserver/              # 程序入口
  internal/
    app/                       # 应用装配与生命周期
    api/                       # HTTP 管理面
    auth/                      # challenge-response 认证
    channel/                   # channel 领域模型
    config/                    # 配置加载
    db/                        # MySQL 连接与迁移执行
    gen/proto/                 # 已生成 protobuf Go 代码
    libp2p/                    # libp2p host 与 identity
    logx/                      # 结构化日志
    media/                     # blob 去重与媒体存储
    message/                   # message 领域模型
    protocol/                  # length-prefixed protobuf 编解码
    repository/                # repository 接口与 MySQL 实现
    space/                     # space 领域模型
    service/                   # 跨领域业务编排
    session/                   # 会话管理与协议分发
    storage/                   # 本地文件存储
  migrations/                  # SQL 迁移
  proto/                       # protobuf 源文件
  scripts/                     # proto 生成与自检脚本
  docker-compose/              # Compose 启动目录与持久化数据目录
```

## 环境变量

核心环境变量如下，完整示例见 [.env.example](/mnt/e/code/meshserver/.env.example)：

- `MESHSERVER_MYSQL_DSN`
- `MESHSERVER_MYSQL_ADMIN_DSN`
- `MESHSERVER_MYSQL_SERVER_PUB_KEY_PATH`
- `MESHSERVER_HTTP_LISTEN_ADDR`
- `MESHSERVER_LIBP2P_LISTEN_ADDRS`
- `MESHSERVER_LIBP2P_PROTOCOL_ID`
- `MESHSERVER_NODE_KEY_PATH`
- `MESHSERVER_BLOB_ROOT`
- `MESHSERVER_LOG_DIR`
- `MESHSERVER_MIGRATIONS_DIR`
- `MESHSERVER_CHALLENGE_TTL`
- `MESHSERVER_MAX_TEXT_LEN`
- `MESHSERVER_MAX_IMAGES_PER_MESSAGE`
- `MESHSERVER_MAX_FILES_PER_MESSAGE`
- `MESHSERVER_MAX_UPLOAD_BYTES`
- `MESHSERVER_DHT_DISCOVERY_NAMESPACE`
- `MESHSERVER_DHT_BOOTSTRAP_PEERS`

配置优先级：

1. 环境变量
2. JSON 配置文件
3. 默认值

Docker Compose 示例配置模板见 [meshserver.json.example](/mnt/e/code/meshserver/docker-compose/data/config/meshserver.json.example)。

`MESHSERVER_DHT_BOOTSTRAP_PEERS` 是可选的 bootstrap 多地址列表，多个地址用逗号分隔。节点启动后会在后台自动尝试连接这些 bootstrap peer，然后再做 DHT bootstrap。

`MESHSERVER_DHT_DISCOVERY_NAMESPACE` 是 DHT 发现使用的 rendezvous 名称，客户端和服务端需要保持一致。默认值是 `meshserver`。

### 客户端连接案例

如果你只想看“客户端如何连上服务端”，可以直接运行：

```bash
go run ./examples/connect-client \
  -discover \
  -discover-namespace meshserver \
  -dht-bootstrap-peer /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW...
```

这个案例会先通过 DHT 找到 meshserver 节点，然后自动连接、完成认证，并打印 Space 列表。

如果你已经知道服务端的 `peer_id`，也可以直接按 `peer_id` 连接：

```bash
go run ./examples/connect-client \
  -node-peer-id 12D3KooW... \
  -dht-bootstrap-peer /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW...
```

这个模式会先用 DHT 根据 `peer_id` 解析出地址，再建立连接并完成认证。

如果你想在连接后顺便查看自己能不能创建顶层 Space，可以加上：

```bash
go run ./examples/connect-client \
  -node-peer-id 12D3KooW... \
  -create-space-permissions \
  -dht-bootstrap-peer /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW...
```

`can_create_space` 和 `can_create_group` 是两个不同的权限：

- `can_create_space` 表示你能不能创建新的顶层 space，不需要指定 space_id
- `can_create_group` 表示你能不能在某个已有 space 里创建 group

如果你想直接创建一个新 space，可以用 `admin-client`：

```bash
go run ./examples/admin-client \
  -node-peer-id 12D3KooW... \
  -create-space=true \
  -new-space-name "AI Example Space" \
  -new-space-description "Created by the admin client example" \
  -new-space-visibility public \
  -new-space-allow-channel-creation=true \
  -dht-bootstrap-peer /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW...
```

## 如何生成 proto

源文件位于 [session.proto](/mnt/e/code/meshserver/proto/meshserver/session/v1/session.proto)。

重新生成命令：

```bash
./scripts/gen-proto.sh
```

生成目标目录：

```text
internal/gen/proto/meshserver/session/v1/
```

当前仓库已经提交了已生成的 Go 文件，因此即使本机没有 `protoc`，也可以直接编译项目。

## 如何本地运行

1. 准备 MySQL 8.4+。
2. 复制 `.env.example` 为 `.env` 并按需修改。
3. 执行：

```bash
make build
make run
```

或者：

```bash
GOMODCACHE=$(pwd)/.gomodcache GOTOOLCHAIN=auto go run ./cmd/meshserver
```

## 如何使用 Docker Compose 启动

1. 进入目录：

```bash
cd docker-compose
```

2. 复制 `.env.example` 为 `.env`：

```bash
cp .env.example .env
```

3. 启动：

```bash
docker compose up -d --build
```

服务包括：

- `mysql`
- `meshserver`

## 数据目录说明

所有运行时数据都存放在 `docker-compose/data/` 下：

- [docker-compose/data/mysql](/mnt/e/code/meshserver/docker-compose/data/mysql)
- [docker-compose/data/blobs](/mnt/e/code/meshserver/docker-compose/data/blobs)
- [docker-compose/data/logs](/mnt/e/code/meshserver/docker-compose/data/logs)
- [docker-compose/data/config](/mnt/e/code/meshserver/docker-compose/data/config)

容器内映射关系：

- `./data/mysql -> /var/lib/mysql`
- `./data/blobs -> /app/data/blobs`
- `./data/logs -> /app/data/logs`
- `./data/config -> /app/data/config`

## 协议说明

主协议：

```text
/meshserver/session/1.0.0
```

传输方式：

- libp2p stream
- 4-byte big-endian length-prefixed protobuf envelope

鉴权签名原文：

```text
protocol_id=<protocol>
client_peer_id=<client-peer-id>
node_peer_id=<node-peer-id>
nonce=<base64>
issued_at_ms=<millis>
expires_at_ms=<millis>
```

启动后默认没有任何 Space、Group 或 Channel。

你需要先通过 `CREATE_SPACE` 创建第一个 Space，再通过 `CREATE_GROUP` 或 `CREATE_CHANNEL` 创建空间内的频道。
认证成功只会创建用户账号，不会自动加入任何 Space。

如果你要写一个 Go + libp2p 的 AI 节点客户端，可以直接参考 [docs/ai-libp2p-client-guide.md](/mnt/e/code/meshserver/docs/ai-libp2p-client-guide.md)。

## 健康检查接口

- `GET /healthz`
- `GET /readyz`
- `GET /version`

如果开启 `MESHSERVER_ENABLE_DEBUG_CONFIG=true`，还会暴露：

- `GET /debug/config`

如果启用 blob 静态路由，可通过 `/blobs/<storage_path>` 访问已存储的附件内容。

## 端口说明

默认端口：

- libp2p TCP: `4001`
- HTTP 管理面: `8080`
- MySQL: `3306`

## 开发辅助

常用命令：

```bash
make proto
make build
make run
make test
make check
make compose-up
make compose-down
```

自检脚本：

```bash
./scripts/check.sh
```

## 常见问题

1. `docker compose up -d --build` 时 MySQL 一直 unhealthy

当前 Compose 使用的是 MySQL 8.4 默认的 `caching_sha2_password`，并通过 Docker 卷保存 MySQL 数据。若你想彻底重置数据库状态，可以先执行：

```bash
cd docker-compose
docker compose down
docker volume rm docker-compose_meshserver_mysql_data
docker compose up -d --build
```

2. 宿主机 `4001` 端口已被占用

默认映射端口仍然是 `4001`。如果本机已有其它 libp2p 服务占用它，可以临时覆盖宿主机映射端口，不改仓库默认值：

```bash
cd docker-compose
MESHSERVER_LIBP2P_PORT=14001 docker compose up -d --build
```

## 已实现范围与未实现范围

已实现范围偏向“最小可运行版本”，优先保证：

1. 可编译
2. 可启动
3. 可连接
4. 可落库
5. 可进行基本消息收发与增量同步

因此当前版本更适合作为后续扩展的基础工程，而不是已经覆盖所有高级社区功能的最终产品。
