# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Infrastructure
make up              # start all Docker services + create Kafka topics
make down            # stop all services and delete volumes

# Code generation (requires protoc, protoc-gen-go, protoc-gen-go-grpc)
make proto           # generate Go from proto/sportsbook/v1/*.proto → gen/

# Build & test
make build           # go build ./cmd/...
make test            # go test ./...
go test ./internal/betacceptance/... -v -run TestLagChecker   # single package/test

# Migrations (infra must be up first)
make migrate-all     # run all 7 DB migrations

# Run individual services
make run-betacceptance / run-wallet / run-odds / run-catalog
make run-bethistory / run-settlement / run-identity / run-marketdata

# Frontend
make frontend-install && make frontend-dev   # React dev server on :3000
```

**Port map** (local postgres occupies 127.0.0.1:5432, so db_bet_acceptance is remapped):

| Service | Host port |
|---|---|
| db_bet_acceptance | **15432** |
| db_wallet | 5433 |
| db_odds | 5434 |
| db_catalog | 5435 |
| db_bet_history | 5436 |
| db_settlement | 5437 |
| db_identity | 5438 |
| PgBouncer | 6432 |
| Kafka | 9092 |
| Redis session/odds/market-status/ratelimit | 6379–6382 |

## Architecture

### Services (each has its own DB and `cmd/` entry point)

| Service | Role |
|---|---|
| **Bet Acceptance** | Synchronous HTTP bet placement; owns `outbox` table |
| **Account & Wallet** | Double-entry ledger; gRPC server |
| **Odds Management** | Consumes `price.update` from `market-data-normalised`; writes odds to DB + Redis + publishes `odds-updated` |
| **Market Catalog** | Consumes `catalog.upsert` + `game.state` from `market-data-normalised`; upserts sports/events/markets/selections/live scores to DB; gRPC + HTTP server |
| **Market Data Ingestion** | Polls external sources (Polymarket, NBA scores); normalises → `market-data-normalised`. Single gateway for all external data. |
| **Bet History** | Consumes `bet-placed`, `bet-settled`; gRPC reads for UI |
| **Settlement** | Consumes `bet-settled`; triggers `wallet.CreditBalance` |
| **Identity/Auth** | JWT issuance; owns users/sessions tables |

### Message bus

Kafka (KRaft, single node in dev). Topics partition by **market_id** for market/odds events, **user_id** for bet/wallet events:

```
market-data-raw          partitions: 12
market-data-normalised   partitions: 12
odds-updated             partitions: 12
odds-suspended           partitions: 12
bet-placed               partitions: 24
bet-settled              partitions: 24
bet-voided               partitions: 24
bet-recorded             partitions: 24
wallet-transaction       partitions: 24
```

### gRPC connections (synchronous)

- Bet Acceptance → Wallet: `DeductBalance`, `GetUserLimits`
- Bet Acceptance → Catalog: `GetMarket`, `GetSelection`
- Bet History → Wallet: `GetBalance`

Proto sources: `proto/sportsbook/v1/`. Generated code: `gen/sportsbook/v1/` (not committed — run `make proto`).

### Redis (4 logical instances)

| Instance | Port | Policy | Used for |
|---|---|---|---|
| redis-session | 6379 | allkeys-lru | JWT session tokens |
| redis-odds | 6380 | volatile-ttl | Odds cache (`odds:{marketID}:{selectionID}`) |
| redis-market-status | 6381 | volatile-ttl | Market status cache (`market:status:{marketID}`) |
| redis-ratelimit | 6382 | allkeys-lru | Idempotency keys, lag check cache |

### PgBouncer

Transaction pooling for all services **except Account & Wallet**, which uses session pooling because `DeductBalance` holds `pg_advisory_xact_lock(hashtext(user_id))` across the transaction.

## Critical design decisions

### Bet acceptance flow (9 steps, `internal/betacceptance/bet_flow.go`)

1. Idempotency check — Redis `idem:{key}` (24h TTL)
2. Market status — Redis `market:status:{id}` (5s TTL); must be `"OPEN"`
3. Odds validation — Redis `odds:{market}:{selection}` (30s TTL); reject if movement > 5%
4. User limits — gRPC `GetUserLimits`
5. **Lag check** — only for stakes ≥ £100 (10,000 minor units); **fail-closed**
6. Write outbox row (`PENDING`)
7. gRPC `DeductBalance`
8. Mark outbox `READY_TO_PUBLISH`
9. Cache idempotency key; return `ACCEPTED`

### Lag check (`internal/betacceptance/lag_checker.go`)

Answers: "Has the `odds-management-cg` consumer group processed all prior `bet-placed` messages on this partition?"

```
lag = client.GetOffset(bet-placed, partition, OffsetNewest)
    - admin.ListConsumerGroupOffsets("odds-management-cg", partition).Offset
```

Cached 200ms in `redis-ratelimit`. **Fail-CLOSED**: any Kafka admin error → `(true, err)` → bet rejected. The `PartitionForMarket` FNV-1a hash **must** match the Kafka producer partitioner.

### Outbox relay (`internal/betacceptance/outbox.go`)

Polls every 100ms for `READY_TO_PUBLISH` rows using `SELECT FOR UPDATE SKIP LOCKED`. Uses an idempotent transactional Sarama producer (`cfg.Producer.Idempotent = true`, `cfg.Producer.Transaction.ID`). If `markPublished` fails after a successful Kafka send, the row stays `READY_TO_PUBLISH` and the broker deduplicates the retry via producer epoch.

**Do not pre-commit the Kafka offset in Odds Management** before writing odds to DB and publishing `odds-updated` — this would make the lag check ineffective.

### Wallet double-entry invariant

`available_minor + bets_in_flight_minor = total_minor` must hold at all times. `DeductBalance` decrements `available` and increments `bets_in_flight` atomically under `pg_advisory_xact_lock`. `CreditBalance` (win/void/push) does the reverse.
