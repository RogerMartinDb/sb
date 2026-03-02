.PHONY: proto build test migrate-all up down

PROTO_DIR := proto
GEN_DIR   := gen

# ── Infrastructure ──────────────────────────────────────────────────────────
up:
	docker compose up -d
	@echo "Waiting for Kafka init..."
	docker compose run --rm kafka-init

down:
	docker compose down -v

# ── Protobuf code generation ─────────────────────────────────────────────────
proto:
	@mkdir -p $(GEN_DIR)
	protoc \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		-I $(PROTO_DIR) \
		$(shell find $(PROTO_DIR) -name '*.proto')

# ── Build all services ────────────────────────────────────────────────────────
build:
	go build ./cmd/...

# ── Run individual services ───────────────────────────────────────────────────
run-betacceptance:
	go run ./cmd/betacceptance

run-wallet:
	go run ./cmd/wallet

run-odds:
	go run ./cmd/odds

run-catalog:
	go run ./cmd/catalog

run-bethistory:
	go run ./cmd/bethistory

run-settlement:
	go run ./cmd/settlement

run-identity:
	go run ./cmd/identity

run-marketdata:
	go run ./cmd/marketdata

# ── Migrations ────────────────────────────────────────────────────────────────
migrate-all: migrate-bet-acceptance migrate-wallet migrate-odds migrate-catalog migrate-bet-history migrate-settlement migrate-identity

migrate-bet-acceptance:
	psql "postgres://sb:sb_secret@localhost:15432/db_bet_acceptance" -f migrations/db_bet_acceptance/001_init.sql

migrate-wallet:
	psql "postgres://sb:sb_secret@localhost:5433/db_wallet" -f migrations/db_wallet/001_init.sql

migrate-odds:
	psql "postgres://sb:sb_secret@localhost:5434/db_odds" -f migrations/db_odds/001_init.sql

migrate-catalog:
	psql "postgres://sb:sb_secret@localhost:5435/db_catalog" -f migrations/db_catalog/001_init.sql

migrate-bet-history:
	psql "postgres://sb:sb_secret@localhost:5436/db_bet_history" -f migrations/db_bet_history/001_init.sql

migrate-settlement:
	psql "postgres://sb:sb_secret@localhost:5437/db_settlement" -f migrations/db_settlement/001_init.sql

migrate-identity:
	psql "postgres://sb:sb_secret@localhost:5438/db_identity" -f migrations/db_identity/001_init.sql

# ── Tests ─────────────────────────────────────────────────────────────────────
test:
	go test ./...

# ── Frontend ──────────────────────────────────────────────────────────────────
frontend-install:
	cd frontend && npm install

frontend-dev:
	cd frontend && npm run dev

frontend-build:
	cd frontend && npm run build
