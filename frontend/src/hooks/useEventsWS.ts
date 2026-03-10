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

function applyOddsUpdate(
  events: Event[],
  msg: OddsUpdateMsg,
): { next: Event[]; changed: { id: string; dir: 'up' | 'down' }[] } {
  let anyChanged = false
  const changed: { id: string; dir: 'up' | 'down' }[] = []
  const next = events.map(ev => {
    let evChanged = false
    const markets = ev.markets.map(m => {
      if (m.market_id !== msg.market_id) return m
      let mChanged = false
      const selections = m.selections.map(s => {
        const update = msg.selections.find(u => u.selection_id === s.selection_id)
        if (!update) return s
        if (s.odds_decimal === update.odds_decimal && s.odds_american === update.odds_american) return s
        changed.push({ id: s.selection_id, dir: update.odds_decimal > s.odds_decimal ? 'up' : 'down' })
        mChanged = true
        return { ...s, odds_decimal: update.odds_decimal, odds_american: update.odds_american }
      })
      if (!mChanged) return m
      evChanged = true
      return { ...m, selections }
    })
    if (!evChanged) return ev
    anyChanged = true
    return { ...ev, markets }
  })
  return { next: anyChanged ? next : events, changed }
}

function applyScoreUpdate(
  events: Event[],
  msg: ScoreUpdateMsg,
): { next: Event[]; changedId: string | null; scoreChanged: boolean } {
  let changedId: string | null = null
  let scoreChanged = false
  const next = events.map(ev => {
    if (ev.event_id !== msg.event_id) return ev
    if (
      ev.home_score === msg.home_score &&
      ev.away_score === msg.away_score &&
      ev.game_period === msg.game_period &&
      ev.game_clock === msg.game_clock &&
      ev.status === msg.status
    ) return ev
    if (ev.home_score !== msg.home_score || ev.away_score !== msg.away_score) {
      scoreChanged = true
    }
    changedId = ev.event_id
    return {
      ...ev,
      home_score: msg.home_score,
      away_score: msg.away_score,
      game_period: msg.game_period,
      game_clock: msg.game_clock,
      status: msg.status,
    }
  })
  return { next: changedId ? next : events, changedId, scoreChanged }
}

const FLASH_DURATION = 1500

export type ScoreFlashSide = 'home' | 'away' | 'both'

export function useEventsWS(): {
  events: Event[]
  loading: boolean
  oddsFlash: Map<string, 'up' | 'down'>
  scoreFlash: Map<string, ScoreFlashSide>
} {
  const [events, setEvents] = useState<Event[]>([])
  const [loading, setLoading] = useState(true)
  const [oddsFlash, setOddsFlash] = useState<Map<string, 'up' | 'down'>>(new Map())
  const [scoreFlash, setScoreFlash] = useState<Map<string, ScoreFlashSide>>(new Map())
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectRef = useRef<ReturnType<typeof setTimeout>>()
  const backoffRef = useRef(1000)
  const eventsRef = useRef<Event[]>([])

  // Fallback: fetch via HTTP if WS is not available.
  const fetchFallback = useCallback(() => {
    getEvents()
      .then(data => {
        const d = data ?? []
        eventsRef.current = d
        setEvents(d)
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
          case 'snapshot': {
            const d = msg.events ?? []
            eventsRef.current = d
            setEvents(d)
            setLoading(false)
            break
          }
          case 'odds_update': {
            const { next, changed } = applyOddsUpdate(eventsRef.current, msg)
            eventsRef.current = next
            setEvents(next)
            if (changed.length) {
              setOddsFlash(f => {
                const m = new Map(f)
                changed.forEach(c => m.set(c.id, c.dir))
                return m
              })
              setTimeout(() => {
                if (unmounted) return
                setOddsFlash(f => {
                  const m = new Map(f)
                  changed.forEach(c => m.delete(c.id))
                  return m
                })
              }, FLASH_DURATION)
            }
            break
          }
          case 'score_update': {
            // Determine which score(s) changed from current eventsRef.
            const cur = eventsRef.current.find(e => e.event_id === msg.event_id)
            const homeChanged = cur != null && cur.home_score !== msg.home_score
            const awayChanged = cur != null && cur.away_score !== msg.away_score
            const flashSide: ScoreFlashSide | null =
              homeChanged && awayChanged ? 'both'
              : homeChanged ? 'home'
              : awayChanged ? 'away'
              : null

            // Use functional updater so React always has the authoritative prev.
            setEvents(prev => {
              const { next } = applyScoreUpdate(prev, msg)
              eventsRef.current = next
              return next
            })

            if (flashSide) {
              const id = msg.event_id
              const side = flashSide
              setScoreFlash(f => new Map([...f, [id, side]]))
              setTimeout(() => {
                if (unmounted) return
                setScoreFlash(f => {
                  const m = new Map(f)
                  m.delete(id)
                  return m
                })
              }, FLASH_DURATION)
            }
            break
          }
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

  return { events, loading, oddsFlash, scoreFlash }
}

