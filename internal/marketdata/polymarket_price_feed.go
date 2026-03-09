package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

const (
	clobWSURL     = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
	clobPingEvery = 10 * time.Second
	maxBackoff    = 60 * time.Second
)

// PolymarketPriceFeed connects to the Polymarket CLOB WebSocket to receive
// real-time price changes for all tracked CLOB tokens.
type PolymarketPriceFeed struct {
	registry *TokenRegistry
	logger   *slog.Logger
}

// NewPolymarketPriceFeed creates a new WS price feed.
// The feed waits for the token registry to have entries before connecting.
func NewPolymarketPriceFeed(registry *TokenRegistry, logger *slog.Logger) *PolymarketPriceFeed {
	return &PolymarketPriceFeed{
		registry: registry,
		logger:   logger,
	}
}

func (f *PolymarketPriceFeed) Subscribe(ctx context.Context) (<-chan RawProviderEvent, error) {
	ch := make(chan RawProviderEvent, 128)
	go func() {
		defer close(ch)
		f.run(ctx, ch)
	}()
	return ch, nil
}

func (f *PolymarketPriceFeed) run(ctx context.Context, ch chan<- RawProviderEvent) {
	// Wait for at least one token to be registered before connecting.
	f.logger.Info("polymarket price ws: waiting for token registry to populate")
	for f.registry.Len() == 0 {
		select {
		case <-ctx.Done():
			return
		case <-f.registry.Changed():
		}
	}
	f.logger.Info("polymarket price ws: token registry ready", "tokens", f.registry.Len())

	backoff := time.Second
	for {
		err := f.connectAndStream(ctx, ch)
		if ctx.Err() != nil {
			return
		}
		f.logger.Error("polymarket price ws: connection lost, reconnecting", "err", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (f *PolymarketPriceFeed) connectAndStream(ctx context.Context, ch chan<- RawProviderEvent) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, clobWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// Reset backoff on successful connect (caller handles backoff, but we
	// signal success by running without immediate error).
	f.logger.Info("polymarket price ws: connected", "url", clobWSURL)

	// Subscribe to all known tokens.
	tokenIDs := f.registry.AllTokenIDs()
	if err := f.sendSubscribe(conn, tokenIDs); err != nil {
		return fmt.Errorf("initial subscribe: %w", err)
	}
	f.logger.Info("polymarket price ws: subscribed", "tokens", len(tokenIDs))

	// Track currently subscribed tokens for diff on registry changes.
	subscribed := make(map[string]struct{}, len(tokenIDs))
	for _, id := range tokenIDs {
		subscribed[id] = struct{}{}
	}

	// Read messages in a goroutine, forward to msgCh.
	msgCh := make(chan []byte, 64)
	errCh := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			select {
			case msgCh <- msg:
			default:
				// Drop if consumer is slow.
			}
		}
	}()

	pingTicker := time.NewTicker(clobPingEvery)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-errCh:
			return fmt.Errorf("read: %w", err)

		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.TextMessage, []byte("PING")); err != nil {
				return fmt.Errorf("ping: %w", err)
			}

		case <-f.registry.Changed():
			f.handleRegistryChange(conn, subscribed)

		case msg := <-msgCh:
			f.handleMessage(msg, ch)
		}
	}
}

// clobSubscribeMsg is the subscription message for the CLOB WS.
type clobSubscribeMsg struct {
	AssetsIDs []string `json:"assets_ids"`
	Type      string   `json:"type,omitempty"`
	Operation string   `json:"operation,omitempty"`
}

func (f *PolymarketPriceFeed) sendSubscribe(conn *websocket.Conn, tokenIDs []string) error {
	msg := clobSubscribeMsg{
		AssetsIDs: tokenIDs,
		Type:      "market",
	}
	return conn.WriteJSON(msg)
}

func (f *PolymarketPriceFeed) handleRegistryChange(conn *websocket.Conn, subscribed map[string]struct{}) {
	current := f.registry.AllTokenIDs()
	currentSet := make(map[string]struct{}, len(current))
	for _, id := range current {
		currentSet[id] = struct{}{}
	}

	// Find new tokens to subscribe.
	var toSub []string
	for _, id := range current {
		if _, ok := subscribed[id]; !ok {
			toSub = append(toSub, id)
			subscribed[id] = struct{}{}
		}
	}

	// Find removed tokens to unsubscribe.
	var toUnsub []string
	for id := range subscribed {
		if _, ok := currentSet[id]; !ok {
			toUnsub = append(toUnsub, id)
			delete(subscribed, id)
		}
	}

	if len(toSub) > 0 {
		msg := clobSubscribeMsg{AssetsIDs: toSub, Operation: "subscribe"}
		if err := conn.WriteJSON(msg); err != nil {
			f.logger.Warn("polymarket price ws: subscribe new tokens failed", "err", err)
		} else {
			f.logger.Info("polymarket price ws: subscribed new tokens", "count", len(toSub))
		}
	}
	if len(toUnsub) > 0 {
		msg := clobSubscribeMsg{AssetsIDs: toUnsub, Operation: "unsubscribe"}
		if err := conn.WriteJSON(msg); err != nil {
			f.logger.Warn("polymarket price ws: unsubscribe tokens failed", "err", err)
		} else {
			f.logger.Info("polymarket price ws: unsubscribed tokens", "count", len(toUnsub))
		}
	}
}

// clobWSEvent is the raw JSON structure from the CLOB WebSocket.
type clobWSEvent struct {
	EventType string          `json:"event_type"`
	AssetID   string          `json:"asset_id"`
	Price     string          `json:"price"`
	Rest      json.RawMessage `json:"-"` // unused fields
}

// wsPriceRaw is the normaliser-facing payload emitted by this feed.
type wsPriceRaw struct {
	ConditionID string `json:"condition_id"`
	SelIndex    int    `json:"sel_index"`
	Price       string `json:"price"`
	ProviderID  string `json:"provider_id"` // original provider, e.g. "polymarket-nba"
}

func (f *PolymarketPriceFeed) handleMessage(msg []byte, ch chan<- RawProviderEvent) {
	text := string(msg)

	// Handle PONG response to our PINGs.
	if text == "PONG" {
		return
	}

	// Try to parse as a JSON array (batch) or single object.
	var events []clobWSEvent
	if len(msg) > 0 && msg[0] == '[' {
		if err := json.Unmarshal(msg, &events); err != nil {
			f.logger.Debug("polymarket price ws: unmarshal array failed", "err", err, "msg", text)
			return
		}
	} else {
		var single clobWSEvent
		if err := json.Unmarshal(msg, &single); err != nil {
			f.logger.Debug("polymarket price ws: unmarshal failed", "err", err, "msg", text)
			return
		}
		events = []clobWSEvent{single}
	}

	now := time.Now().UTC()
	for _, ev := range events {
		if ev.EventType != "price_change" {
			f.logger.Debug("polymarket price ws: ignoring event type", "type", ev.EventType)
			continue
		}
		if ev.AssetID == "" || ev.Price == "" {
			continue
		}

		entry, ok := f.registry.Lookup(ev.AssetID)
		if !ok {
			f.logger.Debug("polymarket price ws: unknown asset_id", "asset_id", ev.AssetID)
			continue
		}

		raw := wsPriceRaw{
			ConditionID: entry.ConditionID,
			SelIndex:    entry.SelIndex,
			Price:       ev.Price,
			ProviderID:  entry.ProviderID,
		}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}

		select {
		case ch <- RawProviderEvent{
			ProviderID: "polymarket-ws-price",
			Data:       data,
			ReceivedAt: now,
		}:
		default:
			// Drop if channel is full.
		}
	}
}
