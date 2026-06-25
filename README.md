# Bunch

Friends, blocks, and presence service for the BananaKit ecosystem.

Part of [BananaLabs](https://github.com/BananaLabs-OSS).

## What It Does

Bunch owns friend relationships, block lists, and online presence. Players connect via WebSocket to go online, and their friends get real-time notifications. Internal services can query who's online via HTTP.

Depends on [BananAuth](https://github.com/bananalabs-oss/bananauth) for identity. Uses shared JWT validation from [Potassium](https://github.com/bananalabs-oss/potassium).

## Endpoints

### Friends (JWT auth)

| Method   | Path                 | Body                       | Description                                 |
| -------- | -------------------- | -------------------------- | ------------------------------------------- |
| `POST`   | `/friends/request`   | `{ "friend_id": "uuid" }`  | Send friend request                         |
| `POST`   | `/friends/accept`    | `{ "request_id": "uuid" }` | Accept friend request                       |
| `POST`   | `/friends/decline`   | `{ "request_id": "uuid" }` | Decline friend request                      |
| `DELETE` | `/friends/:friendId` | —                          | Remove friend                               |
| `GET`    | `/friends`           | —                          | List accepted friends                       |
| `GET`    | `/friends/requests`  | —                          | List pending requests (incoming + outgoing) |

### Blocks (JWT auth)

| Method   | Path                 | Body                       | Description                          |
| -------- | -------------------- | -------------------------- | ------------------------------------ |
| `POST`   | `/blocks`            | `{ "account_id": "uuid" }` | Block user (also removes friendship) |
| `DELETE` | `/blocks/:accountId` | —                          | Unblock user                         |
| `GET`    | `/blocks`            | —                          | List blocked users                   |

### Presence (WebSocket)

| Path            | Auth              | Description                                        |
| --------------- | ----------------- | -------------------------------------------------- |
| `/ws?token=JWT` | JWT (query param) | Connect to go online, receive friend notifications |

WebSocket messages pushed to connected clients:

```json
{"type":"friend_online","account_id":"uuid"}
{"type":"friend_offline","account_id":"uuid"}
```

### Internal (service token)

| Method | Path                         | Body                              | Description             |
| ------ | ---------------------------- | --------------------------------- | ----------------------- |
| `GET`  | `/internal/presence/:userId` | —                                 | Check if user is online |
| `POST` | `/internal/presence/bulk`    | `{ "account_ids": ["uuid",...] }` | Bulk online check       |
| `GET`  | `/internal/presence/count`   | —                                 | Total online players    |

### System

| Method | Path      | Description                          |
| ------ | --------- | ------------------------------------ |
| `GET`  | `/health` | Health check (includes online_count) |

## Config

### Native (`cmd/server`)

| Env Var              | Default              | Description                                                              |
| -------------------- | -------------------- | ------------------------------------------------------------------------ |
| `JWT_SECRET`         | _required_           | Shared JWT signing key (must match BananAuth)                            |
| `SERVICE_SECRET`     | `dev-service-secret` | Service-to-service auth token for `/internal` routes                     |
| `DATABASE_URL`       | `sqlite://bunch.db`  | SQLite database path                                                     |
| `HOST`               | `0.0.0.0`            | Server bind address                                                      |
| `PORT`               | `8002`               | HTTP port                                                                |
| `WS_ALLOWED_ORIGINS` | _(none)_             | Comma-separated origin allowlist for WS upgrades. Empty = deny all. `*` = allow all (dev only). |

### Pulp cell (`pulp-cell`)

Configuration is provided via the `[config]` block in `pulp.cell.toml`:

| Key              | Default              | Description                                   |
| ---------------- | -------------------- | --------------------------------------------- |
| `jwt_secret`     | _required_           | Shared JWT signing key                        |
| `service_secret` | `dev-service-secret` | Service-to-service auth token (also accepts legacy `service_token`) |

The cell has no `WS_ALLOWED_ORIGINS` equivalent — origin checking is handled at the Pulp host layer.

## Run

### Native

```bash
JWT_SECRET=your-secret WS_ALLOWED_ORIGINS=https://yourapp.com go run ./cmd/server
```

### Pulp cell

Build the cell, then launch with the deployment host:

```bash
cd pulp-cell
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o bunch.wasm .
cd ../pulp-deployment
go run . --cell ../pulp-cell/bunch.wasm
```

## Docker

```bash
docker pull ghcr.io/bananalabs-oss/bunch:v0.2.0
docker run -p 8002:8002 -e JWT_SECRET=your-secret -e WS_ALLOWED_ORIGINS=https://yourapp.com bunch
```
