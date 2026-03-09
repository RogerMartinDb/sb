# Sportsbook

A sports betting platform built with Go microservices, Kafka, PostgreSQL, Redis, and a React frontend.

## Architecture

Eight independent services, each with its own database, communicating via Kafka (async) and gRPC (sync).

```
                    ┌────────────────────────────────────────────────┐
                    │  React Frontend (:3000)                        │
                    └──────────┬──────────────────┬──────────────────┘
                         HTTP  │                  │ HTTP
                               │                  │
          ┌────────────────────▼───┐    ┌─────────▼──────────┐
          │  Bet Acceptance (:8080)│    │ Identity/Auth(:8084)│
          │  ─gRPC─▶ Wallet       │    │ JWT issuance        │
          │  ─gRPC─▶ Catalog      │    └─────────────────────┘
          │  outbox ─▶ bet.placed  │
          └────────────────────────┘
                                                                 ┌────────────────┐
 External sources                                                │                │
 ─────────────────                                               │  Market        │
 Polymarket API ──┐   ┌──────────────┐  market-data.normalised   │  Catalog       │
                  ├──▶│  Market Data  ├──────────────────────┬───▶│  (:8086/50052) │
 NBA Scores API ──┘   │  Ingestion   │                      │    │                │
                      └──────────────┘                      │    │  catalog.upsert│
                                                            │    │  game.state    │
                                              ┌─────────────▼─┐  └──────┬─────────┘
                                              │ Odds Mgmt     │         │
                                              │ price.update   │  ┌──────▼─────────┐
                                              └───────┬───────┘  │ HTTP /events    │
                                                      │          │ + Redis odds    │
                                            odds.updated         └────────────────┘
                                                      │
                                              ┌───────▼───────┐
                                              │ Redis odds    │
                                              │ cache (:6380) │
                                              └───────────────┘

           bet.placed / bet.settled
 ┌────────────────────┐    ┌──────────────┐    ┌───────────────┐
 │  Bet History       │    │  Settlement  │    │ Account &     │
 │  (reads gRPC)      │    │  credits     │    │ Wallet (:9091)│
 └────────────────────┘    │  wallet      │    │ gRPC server   │
                           └──────────────┘    └───────────────┘
```

### Data flow

The **Market Data Ingestion** service is the single gateway for all external data. It polls upstream sources, normalises events into a canonical schema, and publishes to the `market-data.normalised` Kafka topic. Downstream services consume from this topic:

```
Polymarket API ──┐
                 ├─▶ PolymarketFeed ──▶ PolymarketNormaliser ──┐
                 │                                              │
NBA Scores API ──┴─▶ NBAScoreFeed ────▶ NBAScoreNormaliser ────┤
                                                                ▼
                                              market-data.normalised
                                                    │         │
                                                    ▼         ▼
                                              Catalog    Odds Management
                                              consumer   (price.update)
                                              (catalog.upsert,
                                               game.state)
```

**Event types on `market-data.normalised`:**

| Event type | Producer | Consumer | Purpose |
|---|---|---|---|
| `catalog.upsert` | Polymarket feed | Catalog | Upsert sport / competition / event / market / selection rows |
| `price.update` | Polymarket feed | Odds Management | Triggers odds recomputation from feed probabilities |
| `game.state` | NBA score feed | Catalog | Updates live scores, period, clock, event status |

### Services

| Service | Entry point | Port | Role |
|---|---|---|---|
| Bet Acceptance | `cmd/betacceptance` | 8080 | HTTP bet placement, outbox relay |
| Account & Wallet | `cmd/wallet` | 9091 | Double-entry ledger, gRPC server |
| Market Catalog | `cmd/catalog` | 8086 (HTTP), 50052 (gRPC) | Market/selection metadata; consumes `market-data.normalised` for catalog upserts and live game state |
| Market Data Ingestion | `cmd/marketdata` | — | Polls external sources (Polymarket, NBA), normalises, publishes to Kafka |
| Odds Management | `cmd/odds` | — | Consumes `price.update`, computes vigged odds, writes Redis + publishes `odds.updated` |
| Bet History | `cmd/bethistory` | 8082 | Consumes bet events, gRPC reads for UI |
| Settlement | `cmd/settlement` | — | Consumes `bet.settled`, credits wallet |
| Identity/Auth | `cmd/identity` | 8084 | JWT issuance, user sessions |

### Tech stack

- **Go 1.23** — all services
- **Kafka** (Confluent Platform 7.9, KRaft) — async event bus
- **PostgreSQL 16** — one DB per service, migrations in `migrations/`
- **PgBouncer** — connection pooling (transaction mode for most; session mode for Wallet)
- **Redis** x 4 — session tokens, odds cache, market-status cache, rate-limit/idempotency
- **gRPC + protobuf** — sync calls between services
- **React + Vite + TypeScript** — frontend

## Prerequisites

- Docker + Docker Compose
- Go 1.23+
- `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc` (for proto generation)
- Node.js 20+ (for frontend)

## Getting started

```bash
# 1. Start all infrastructure
make up

# 2. Generate Go from proto definitions
make proto

# 3. Tidy dependencies
go mod tidy

# 4. Run database migrations
make migrate-all

# 5. Build all services
make build

# 6. Start services (each in a separate terminal)
make run-marketdata        # must start first — publishes to Kafka
make run-catalog           # consumes from Kafka, serves HTTP + gRPC
make run-odds
make run-wallet
make run-betacceptance
make run-bethistory
make run-settlement
make run-identity

# 7. Start the frontend
make frontend-install
make frontend-dev          # http://localhost:3000
```

## Port reference

| Component | Host port |
|---|---|
| db_bet_acceptance | 15432 |
| db_wallet | 5433 |
| db_odds | 5434 |
| db_catalog | 5435 |
| db_bet_history | 5436 |
| db_settlement | 5437 |
| db_identity | 5438 |
| PgBouncer | 6432 |
| Kafka | 9092 |
| redis-session | 6379 |
| redis-odds | 6380 |
| redis-market-status | 6381 |
| redis-ratelimit | 6382 |

> `db_bet_acceptance` is remapped to 15432 because the host machine already uses 5432.

## Key design decisions

### Market data pipeline

All external data enters through the **Market Data Ingestion** service. Provider-specific feeds implement the `ProviderFeed` interface and emit raw events. Provider-specific normalisers convert these into the canonical `NormalisedMarketEvent` schema. A `CompositeNormaliser` dispatches to the right normaliser based on `ProviderID`.

Current feeds:
- **PolymarketFeed** — polls Polymarket Gamma API every 5 min for NBA game events (moneyline, spreads, totals). Emits `catalog.upsert` + `price.update` events.
- **NBAScoreFeed** — polls `cdn.nba.com` scoreboard every 60 s for live scores, period, clock. Emits `game.state` events. Uses a shared `EventMatcher` (populated by the Polymarket feed) to map team names to event IDs.
- **SportradarFeed** — stub for future integration.

The **Catalog** service consumes `market-data.normalised` and handles `catalog.upsert` (upserts sport/competition/event/market/selection rows) and `game.state` (updates live scores). It does **not** produce to Kafka — it is a pure consumer + gRPC/HTTP server.

### Live games

Events have a `status` column (`SCHEDULED`, `LIVE`, `FINISHED`). The Polymarket normaliser sets `LIVE` when the game start time has passed. The NBA score normaliser updates scores, period, clock, and transitions status to `FINISHED` when the game ends. The frontend groups live games into their own section at the top with a pulsing indicator, score display, and period/clock.

### Bet acceptance flow

Placing a bet runs nine synchronous steps (`internal/betacceptance/bet_flow.go`):

1. Idempotency check — Redis `idem:{key}` (24 h TTL)
2. Market status — Redis `market:status:{id}` (5 s TTL); must be `OPEN`
3. Odds validation — Redis `odds:{market}:{selection}` (30 s TTL); reject if movement > 5 %
4. User limits — gRPC `GetUserLimits`
5. Lag check — for stakes >= 100 GBP only; fail-closed
6. Write outbox row (`PENDING`)
7. gRPC `DeductBalance`
8. Mark outbox `READY_TO_PUBLISH`
9. Cache idempotency key; return `ACCEPTED`

### Lag check

Before accepting a large bet, Bet Acceptance verifies that the Odds Management consumer group is not lagging behind on the `bet.placed` topic partition for this market. This prevents a bet being accepted against stale odds.

```
lag = latestOffset(bet.placed, partition) - committedOffset(odds-management-cg, partition)
```

Cached for 200 ms. **Fail-closed**: any Kafka admin error rejects the bet.

### Outbox relay

The relay goroutine polls every 100 ms for `READY_TO_PUBLISH` rows using `SELECT FOR UPDATE SKIP LOCKED` and publishes to Kafka with a transactional idempotent producer. If `markPublished` fails after a successful send, the broker deduplicates the retry via producer epoch.

### Wallet invariant

`available_minor + bets_in_flight_minor = total_minor` must hold at all times. `DeductBalance` decrements `available` and increments `bets_in_flight` atomically under `pg_advisory_xact_lock(hashtext(user_id))`.

## Kafka topics

| Topic | Partitions | Partition key |
|---|---|---|
| market-data.raw | 12 | market_id |
| market-data.normalised | 12 | market_id (or event_id for game.state) |
| odds.updated | 12 | market_id |
| odds.suspended | 12 | market_id |
| bet.placed | 24 | user_id |
| bet.settled | 24 | user_id |
| bet.voided | 24 | user_id |
| bet.recorded | 24 | user_id |
| wallet.transaction | 24 | user_id |

## Project structure

```
.
├── cmd/                    # Service entry points
├── internal/
│   ├── betacceptance/      # Bet placement logic, lag checker, outbox relay
│   ├── wallet/             # Double-entry ledger, gRPC service
│   ├── catalog/            # Market catalog: gRPC + HTTP server, Kafka consumer
│   ├── odds/               # Odds cache updater
│   ├── oddsmanagement/     # Odds computation + publishing
│   ├── bethistory/         # Bet event consumer + gRPC reads
│   ├── settlement/         # Settlement consumer
│   ├── marketdata/         # Feed adapters, normalisers, ingestion service
│   ├── polymarket/         # Polymarket Gamma API client
│   └── identity/           # JWT auth, user management
├── proto/sportsbook/v1/    # Protobuf definitions
├── gen/sportsbook/v1/      # Generated Go code (run make proto)
├── migrations/             # Per-service SQL migrations
├── frontend/               # React + Vite app
├── docker-compose.yml
└── Makefile
```

## Development commands

```bash
make up              # Start all Docker services + create Kafka topics
make down            # Stop all services and delete volumes
make proto           # Generate Go from proto definitions
make build           # go build ./cmd/...
make test            # go test ./...
make migrate-all     # Run all DB migrations (infra must be up)
```
