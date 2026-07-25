# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Concrnt is a federated social-network protocol/server ("makes social media accounts your
internet identities"). This repository (`github.com/concrnt/concrnt`) is the **2.0
rewrite** — a clean re-implementation of the protocol described by the
[CIPs (Concrnt Improvement Proposals)](references/CIPs-translated), designed to be simple
enough to be reimplemented by third parties (even as a static host / serverless writer).

Two directories are historical/reference material, not part of the active codebase:
- `legacy/` — the old v1 data model, imported only by `cmd/conctl/migrate-v1-to-v2.go`
  to support migrating v1 servers to v2.
- `references/concurrent/` — a full, separately-moduled (own `go.mod`) checkout of the
  v1 server kept purely as a design reference while porting behavior to v2. It is not
  built as part of this module.

## Common commands

```sh
# Build the two binaries
go build ./cmd/concrnt   # the server
go build ./cmd/conctl    # admin/CLI tool (account creation, key generation, v1->v2 migration)

# Vet / test
go vet ./...
go test ./...                                    # note: repository-layer tests spin up Postgres via dockertest (needs a working Docker daemon)
go test ./policy/... ./chunkline/... ./client/... # pure-logic packages, no Docker required
go test ./... -run TestName -v                    # run a single test

# Local full stack (server + Postgres + Redis + memcached + web UI)
docker compose up
```

There is no Makefile or lint config in the repo; `go build`/`go vet`/`go test` are the
tools of record. `cmd/concrnt/Dockerfile` is the canonical build recipe used by CI
(`.github/workflows/docker-publish.yml`), which builds and publishes multi-arch images
on tags (`v*.*.*`) and pushes to `main`.

Repository-layer tests (`internal/infra/repository/postgres/*_test.go`) use
`internal/testutil` (`ory/dockertest`) to launch throwaway Postgres containers per test —
expect them to be slow and to require Docker.

## Architecture

The server (`cmd/concrnt/main.go`) is a single Echo (`labstack/echo`) HTTP process wired
up by hand in `main()` (no DI framework). Layering, outside-in:

```
present/rest (Echo handlers)  ->  usecase (business logic, interfaces for deps)  ->  infra/repository (Postgres/GORM)
                                        |                                              infra/gateway (chunkline HTTP gateway)
                                        v                                              infra/pubsub (Redis pub/sub)
                                     domain (entities, Config, sentinel errors)
```

- **`internal/domain`** — core types with no persistence/transport concerns: `Entity`,
  `Record`, `Config`, and sentinel errors (`NotFoundError`, `PermissionError`,
  `RedirectError`, `ValidationError`), each implementing `Is()` for `errors.Is` matching.
- **`internal/usecase`** — one file per bounded concern (`record.go`, `residence.go`,
  `server.go`, `notification.go`, `subscription.go`, `chunkline.go`, `abuse.go`). Each
  usecase declares the repository/gateway interfaces it needs (e.g. `RecordRepository`,
  `SignalService`, `PolicyService` in `record.go`) rather than depending on concrete infra
  types — infra packages satisfy these interfaces structurally.
- **`internal/infra/repository/postgres`** — GORM-backed implementations of the usecase
  repository interfaces.
- **`internal/infra/gateway`** — outbound HTTP to other domains/servers (e.g. chunkline
  federation).
- **`internal/infra/pubsub`** — Redis-backed realtime pub/sub (`NewRedisPubsub`), consumed
  by `worker.Subscriber` for cross-server realtime propagation and by `usecase` for
  publishing events.
- **`internal/present/rest`** — `Handler` (`handler.go`) registers the full CCAPI route
  set; `wellknown.go` serves `/.well-known/concrnt`; `proxy.go` reverse-proxies to
  registered "services" (modules); `middleware/` holds auth (`AuthMiddleware`, subkey and
  ecrecover verification) and recaptcha middleware; `presenter/` maps domain types to
  wire-format responses.
- **`internal/service`** — cross-cutting singletons wired in `main()`: `ModuleManager`
  (polls configured `services` for a `/cc-info` endpoint map and merges it with the base
  REST endpoints once a minute — this is how "modules"/plugins register additional API
  routes without being compiled in) and `PolicyService` (wraps the `policy` package's
  evaluator with global policy + parameters).
- **`internal/worker`** — background loops: `Subscriber` (subscribes to realtime channels
  across the federation), `NotificationReactor` (turns notifications into Web Push via
  `webpush-go`, gated on `vapidPublicKey`/`vapidPrivateKey` being configured).
- **`policy/`** — the CIP-12 policy evaluation engine: policies are stacks of layers, each
  layer a set of statements (`action`, glob-style `key` match, boolean `Condition` expr,
  `emit` conclusion). `EvaluateStack` folds layers with `UNSET`/`ALLOW`/`DENY` combining
  logic (first non-UNSET `DENY` or `ALLOW` short-circuits); `EvaluatePolicy`/`Eval` handle
  a single policy's statements and boolean expression tree respectively.
- **`chunkline/`** — the timeline/feed data structure: a `Manifest` describes chunked,
  paginated timeline storage (ascending/descending endpoints, chunk size); `Client` merges
  multiple remote timelines into a single time-ordered stream via a heap-based priority
  queue (`QueryDescending`), calling out to a `Resolver` interface for manifests, removed
  items, and chunk bodies — this is what lets timelines be federated/paginated across
  domains without loading everything into memory.
- **`client/`** — outbound HTTP client for talking to other concrnt domains/servers
  (fetching signed documents, resolving CCKV/CCFS URIs, realtime websocket subscriptions),
  used by both `usecase` and `chunkline` gateways.
- **`cdid/`** — CDID: a sortable, base32-encoded 25-byte content/timeline chunk ID
  (`KindTime` embeds a timestamp for chunk/record IDs; `KindHash` derives from content
  hash), the ID scheme used throughout records/timelines/chunks.
- **`impl/tags`** — small parser for the entity `tag` string format (`Entity.Tag()`).
- **`impl/interop`** — types/constants shared with "service" modules that plug into
  `ModuleManager` via the `/cc-info` protocol.
- **Root package `concrnt`** (`types.go`, `utils.go`, `crypto.go`) — the wire-level
  protocol types shared across every layer: `SignedDocument`/`Document[T]`/`Proof` (the
  CIP-1 document envelope and its ecrecover/subkey/document-reference proof types),
  `Policy`/`PolicyEntry` (CIP-12), `Event`/`RealtimeRequest` (realtime pub/sub payloads),
  `CCURI` (parses/formats `cckv://` and `ccfs://` URIs), and secp256k1 signing/verification
  built on `go-ethereum`'s crypto plus `cosmos-sdk`'s key types.

### Config

`internal/infra/config` loads YAML (see `config.example.yaml` for the full shape:
`concrnt` domain identity/registration mode, `backends` for Postgres/Redis/Memcached DSNs,
`observability` tracing, `integrations` for captcha/VAPID keys, `meta` for
instance-branding served via well-known, `services` for pluggable modules proxied by
`ModuleManager`/`Proxy`). `domain.Config` (`internal/domain/config.go`) is the resulting
in-process representation (`FQDN`, `PrivateKey`, `Registration`: open/invite/close,
`Layer`, `CSID`).

### Migration

`cmd/conctl migrate-v1-to-v2` (backed by `legacy/core` + `legacy/cdid`) exports a v1
identity's full log and re-imports it into a v2 server — this is the "commit mode"
import path referenced in the project README's release checklist.
