import { useState, useEffect, useRef, useCallback } from 'react'
import { type Event, getEvents } from '../api'

interface OddsUpdateMsg {
  type: 'odds_update'
  market_id: string
  selections: { selection_id: string; odds_decimal: number; odds_american: number }[]
}

interface ScoreUpdateMsg {
  type: 'score_update'
  event_id: string
  home_score: number
  away_score: number
  game_period: string
  game_clock: string
  status: string
}

interface SnapshotMsg {
  type: 'snapshot'
  events: Event[]
}

type WSMessage = SnapshotMsg | OddsUpdateMsg | ScoreUpdateMsg

function applyOddsUpdate(events: Event[], msg: OddsUpdateMsg): Event[] {
  let changed = false
  const next = events.map(ev => {
    let evChanged = false
    const markets = ev.markets.map(m => {
      if (m.market_id !== msg.market_id) return m
      let mChanged = false
      const selections = m.selections.map(s => {
        const update = msg.selections.find(u => u.selection_id === s.selection_id)
        if (!update) return s
        if (s.odds_decimal === update.odds_decimal && s.odds_american === update.odds_american) return s
        mChanged = true
        return { ...s, odds_decimal: update.odds_decimal, odds_american: update.odds_american }
      })
      if (!mChanged) return m
      evChanged = true
      return { ...m, selections }
    })
    if (!evChanged) return ev
    changed = true
    return { ...ev, markets }
  })
  return changed ? next : events
}

function applyScoreUpdate(events: Event[], msg: ScoreUpdateMsg): Event[] {
  let changed = false
  const next = events.map(ev => {
    if (ev.event_id !== msg.event_id) return ev
    if (
      ev.home_score === msg.home_score &&
      ev.away_score === msg.away_score &&
      ev.game_period === msg.game_period &&
      ev.game_clock === msg.game_clock &&
      ev.status === msg.status
    ) return ev
    changed = true
    return {
      ...ev,
      home_score: msg.home_score,
      away_score: msg.away_score,
      game_period: msg.game_period,
      game_clock: msg.game_clock,
      status: msg.status,
    }
  })
  return changed ? next : events
}

export function useEventsWS(): { events: Event[]; loading: boolean } {
  const [events, setEvents] = useState<Event[]>([])
  const [loading, setLoading] = useState(true)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectRef = useRef<ReturnType<typeof setTimeout>>()
  const backoffRef = useRef(1000)

  // Fallback: fetch via HTTP if WS is not available.
  const fetchFallback = useCallback(() => {
    getEvents()
      .then(data => {
        setEvents(data ?? [])
        setLoading(false)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    let unmounted = false

    function connect() {
      if (unmounted) return

      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
      const ws = new WebSocket(`${proto}//${location.host}/catalog-api/ws`)
      wsRef.current = ws

      ws.onopen = () => {
        backoffRef.current = 1000
      }

      ws.onmessage = (event) => {
        if (unmounted) return
        let msg: WSMessage
        try {
          msg = JSON.parse(event.data)
        } catch {
          return
        }

        switch (msg.type) {
          case 'snapshot':
            setEvents(msg.events ?? [])
            setLoading(false)
            break
          case 'odds_update':
            setEvents(prev => applyOddsUpdate(prev, msg))
            break
          case 'score_update':
            setEvents(prev => applyScoreUpdate(prev, msg))
            break
        }
      }

      ws.onclose = () => {
        if (unmounted) return
        reconnectRef.current = setTimeout(() => {
          connect()
        }, backoffRef.current)
        backoffRef.current = Math.min(backoffRef.current * 2, 30000)
      }

      ws.onerror = () => {
        if (unmounted) return
        // If we never got a snapshot (still loading), fall back to HTTP.
        if (loading) {
          fetchFallback()
        }
        ws.close()
      }
    }

    connect()

    return () => {
      unmounted = true
      if (reconnectRef.current) clearTimeout(reconnectRef.current)
      const ws = wsRef.current
      if (ws) {
        ws.onclose = null
        ws.onerror = null
        if (ws.readyState === WebSocket.CONNECTING) {
          // Close after the handshake completes to avoid "closed before established" error
          ws.onopen = () => ws.close()
        } else {
          ws.close()
        }
      }
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  return { events, loading }
}
