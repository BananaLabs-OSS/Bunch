# Bunch

Friends + Presence service for the BananaKit ecosystem.

Part of [BananaLabs](https://github.com/BananaLabs-OSS).

## What It Does

Bunch owns friend relationships and (eventually) online presence. Any service in the ecosystem can query Bunch to resolve social connections.

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

### System

| Method | Path      | Description  |
| ------ | --------- | ------------ |
| `GET`  | `/health` | Health check |

## Config

| Env Var          | Default              | Description                                   |
| ---------------- | -------------------- | --------------------------------------------- |
| `JWT_SECRET`     | _required_           | Shared JWT signing key (must match BananAuth) |
| `SERVICE_SECRET` | `dev-service-secret` | Service-to-service auth token                 |
| `DATABASE_URL`   | `sqlite://bunch.db`  | SQLite database path                          |
| `HOST`           | `0.0.0.0`            | Server bind address                           |
| `PORT`           | `8002`               | HTTP port                                     |

## Run

```bash
JWT_SECRET=your-secret go run ./cmd/server
```

## Docker

```bash
docker build -t bunch .
docker run -p 8002:8002 -e JWT_SECRET=your-secret bunch
```
