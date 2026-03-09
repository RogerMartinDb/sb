package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// HTTPHandler serves the catalog REST API.
type HTTPHandler struct {
	db          *pgxpool.Pool
	redisOdds   *redis.Client // redis-odds on port 6380
	broadcaster *Broadcaster
	logger      *slog.Logger
}

func NewHTTPHandler(db *pgxpool.Pool, redisOdds *redis.Client, broadcaster *Broadcaster, logger *slog.Logger) *HTTPHandler {
	return &HTTPHandler{db: db, redisOdds: redisOdds, broadcaster: broadcaster, logger: logger}
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h *HTTPHandler) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", h.handleGetEvents)
	mux.HandleFunc("GET /ws", h.handleWS)
	return mux
}

// ── Response types ──────────────────────────────────────────────────────────

type eventResp struct {
	EventID       string       `json:"event_id"`
	CompetitionID string       `json:"competition_id"`
	Name          string       `json:"name"`
	StartsAt      string       `json:"starts_at"`
	Status        string       `json:"status"`
	HomeScore     int          `json:"home_score"`
	AwayScore     int          `json:"away_score"`
	GamePeriod    string       `json:"game_period"`
	GameClock     string       `json:"game_clock"`
	Markets       []marketResp `json:"markets"`
}

type marketResp struct {
	MarketID    string          `json:"market_id"`
	Name        string          `json:"name"`
	Status      string          `json:"status"`
	MarketType  string          `json:"market_type"`
	TargetValue float64         `json:"target_value"`
	IsMain      bool            `json:"is_main"`
	Selections  []selectionResp `json:"selections"`
}

type selectionResp struct {
	SelectionID  string  `json:"selection_id"`
	Name         string  `json:"name"`
	TargetValue  float64 `json:"target_value"`
	OddsDecimal  float64 `json:"odds_decimal"`
	OddsAmerican int     `json:"odds_american"`
}

// ── Handler ─────────────────────────────────────────────────────────────────

func (h *HTTPHandler) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.buildEventsSnapshot(r.Context())
	if err != nil {
		h.logger.Error("http: build events snapshot failed", "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// handleWS upgrades to WebSocket, sends a snapshot, then reads until disconnect.
func (h *HTTPHandler) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("ws: upgrade failed", "err", err)
		return
	}

	// Send initial snapshot.
	events, err := h.buildEventsSnapshot(r.Context())
	if err != nil {
		h.logger.Error("ws: build snapshot failed", "err", err)
		conn.Close()
		return
	}

	snapshot := struct {
		Type   string      `json:"type"`
		Events []eventResp `json:"events"`
	}{
		Type:   "snapshot",
		Events: events,
	}
	if err := conn.WriteJSON(snapshot); err != nil {
		h.logger.Error("ws: send snapshot failed", "err", err)
		conn.Close()
		return
	}

	// Register with broadcaster for live updates.
	h.broadcaster.AddClient(conn)

	// Read loop: just detect disconnects.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	h.broadcaster.RemoveClient(conn)
}

// buildEventsSnapshot queries the DB and Redis to produce the full events list.
func (h *HTTPHandler) buildEventsSnapshot(ctx context.Context) ([]eventResp, error) {
	rows, err := h.db.Query(ctx, `
		SELECT
			e.event_id, e.competition_id, e.name, e.starts_at::text, e.status,
			e.home_score, e.away_score, e.game_period, e.game_clock,
			m.market_id, m.name, m.status, m.market_type, m.target_value, m.is_main,
			s.selection_id, s.name, s.target_value
		FROM events e
		JOIN markets m ON m.event_id = e.event_id
		JOIN selections s ON s.market_id = m.market_id
		WHERE e.status IN ('SCHEDULED', 'LIVE')
		  AND m.status = 'OPEN'
		  AND s.active = true
		ORDER BY
		    CASE e.status WHEN 'LIVE' THEN 0 ELSE 1 END,
		    e.starts_at, e.event_id, m.is_main DESC, m.market_type, m.target_value, s.selection_id`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	eventMap := map[string]*eventResp{}
	marketMap := map[string]*marketResp{}
	marketEvent := map[string]string{} // market_id → event_id
	var eventOrder []string
	var marketOrder []string
	var selKeys []selKey

	for rows.Next() {
		var (
			eID, eCompID, eName, eStartsAt, eStatus string
			eHomeScore, eAwayScore                  int
			eGamePeriod, eGameClock                 string
			mID, mName, mStatus, mType              string
			sID, sName                              string
			mTargetValue, sTargetValue              float64
			mIsMain                                 bool
		)
		if err := rows.Scan(
			&eID, &eCompID, &eName, &eStartsAt, &eStatus,
			&eHomeScore, &eAwayScore, &eGamePeriod, &eGameClock,
			&mID, &mName, &mStatus, &mType, &mTargetValue, &mIsMain,
			&sID, &sName, &sTargetValue,
		); err != nil {
			h.logger.Error("http: scan row failed", "err", err)
			continue
		}

		if _, ok := eventMap[eID]; !ok {
			eventMap[eID] = &eventResp{
				EventID:       eID,
				CompetitionID: eCompID,
				Name:          eName,
				StartsAt:      eStartsAt,
				Status:        eStatus,
				HomeScore:     eHomeScore,
				AwayScore:     eAwayScore,
				GamePeriod:    eGamePeriod,
				GameClock:     eGameClock,
			}
			eventOrder = append(eventOrder, eID)
		}

		if _, ok := marketMap[mID]; !ok {
			marketMap[mID] = &marketResp{
				MarketID:    mID,
				Name:        mName,
				Status:      mStatus,
				MarketType:  mType,
				TargetValue: mTargetValue,
				IsMain:      mIsMain,
			}
			marketEvent[mID] = eID
			marketOrder = append(marketOrder, mID)
		}

		marketMap[mID].Selections = append(marketMap[mID].Selections, selectionResp{
			SelectionID: sID,
			Name:        sName,
			TargetValue: sTargetValue,
		})
		selKeys = append(selKeys, selKey{mID, sID})
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("http: rows error", "err", err)
	}

	// Fetch odds from Redis and inject into selections.
	oddsMap := h.fetchOdds(ctx, selKeys)
	for _, mk := range marketMap {
		for i := range mk.Selections {
			key := mk.MarketID + ":" + mk.Selections[i].SelectionID
			if o, ok := oddsMap[key]; ok {
				mk.Selections[i].OddsDecimal = o.decimal
				mk.Selections[i].OddsAmerican = o.american
			}
		}
	}

	// Assemble markets into events in order.
	for _, mID := range marketOrder {
		eID := marketEvent[mID]
		eventMap[eID].Markets = append(eventMap[eID].Markets, *marketMap[mID])
	}

	result := make([]eventResp, 0, len(eventOrder))
	for _, eID := range eventOrder {
		result = append(result, *eventMap[eID])
	}
	return result, nil
}

type selKey struct{ marketID, selectionID string }

type oddsVal struct {
	decimal  float64
	american int
}

func (h *HTTPHandler) fetchOdds(ctx context.Context, keys []selKey) map[string]oddsVal {
	result := make(map[string]oddsVal, len(keys))
	if len(keys) == 0 {
		return result
	}

	redisKeys := make([]string, len(keys))
	for i, k := range keys {
		redisKeys[i] = fmt.Sprintf("odds:%s:%s", k.marketID, k.selectionID)
	}

	vals, err := h.redisOdds.MGet(ctx, redisKeys...).Result()
	if err != nil {
		h.logger.Error("http: redis MGET failed", "err", err)
		return result
	}

	for i, v := range vals {
		if v == nil {
			continue
		}
		str, ok := v.(string)
		if !ok {
			continue
		}
		var o struct {
			Decimal  float64 `json:"decimal"`
			American int     `json:"american"`
		}
		if err := json.Unmarshal([]byte(str), &o); err != nil {
			continue
		}
		key := keys[i].marketID + ":" + keys[i].selectionID
		result[key] = oddsVal{decimal: o.Decimal, american: o.American}
	}
	return result
}
