import { useState, useEffect } from 'react'
import { getEvents, type Event, type Market, type Selection } from '../api'

export interface SelectedBet {
  market_id: string
  market_name: string
  selection_id: string
  selection_name: string
  odds_decimal: number
  odds_american: number
}

interface Props {
  onSelectBet: (bet: SelectedBet) => void
}

function formatAmerican(n: number): string {
  return n >= 0 ? `+${n}` : `${n}`
}

function formatLine(sel: Selection, market: Market): string {
  switch (market.market_type) {
    case 'SPREAD': {
      const sign = sel.target_value > 0 ? '+' : ''
      return `${sel.name} ${sign}${sel.target_value}`
    }
    case 'TOTAL': {
      const label = sel.target_value > 0 ? 'Over' : 'Under'
      return `${label} ${Math.abs(sel.target_value)}`
    }
    default:
      return sel.name
  }
}

export default function EventList({ onSelectBet }: Props) {
  const [events, setEvents] = useState<Event[]>([])
  const [expandedEvents, setExpandedEvents] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    const load = () => {
      getEvents()
        .then(data => { if (active) setEvents(data ?? []) })
        .catch(() => {})
        .finally(() => { if (active) setLoading(false) })
    }
    load()
    const interval = setInterval(load, 30_000)
    return () => { active = false; clearInterval(interval) }
  }, [])

  function toggleExpand(eventId: string) {
    setExpandedEvents(prev => {
      const next = new Set(prev)
      if (next.has(eventId)) next.delete(eventId)
      else next.add(eventId)
      return next
    })
  }

  function handleSelect(market: Market, sel: Selection) {
    if (sel.odds_decimal <= 0) return
    onSelectBet({
      market_id: market.market_id,
      market_name: market.name,
      selection_id: sel.selection_id,
      selection_name: formatLine(sel, market),
      odds_decimal: sel.odds_decimal,
      odds_american: sel.odds_american,
    })
  }

  if (loading) return <p>Loading events...</p>
  if (events.length === 0) return <p>No upcoming events.</p>

  return (
    <div>
      {events.map(ev => {
        const mainMarkets = ev.markets.filter(m => m.is_main)
        const altMarkets = ev.markets.filter(m => !m.is_main)
        const expanded = expandedEvents.has(ev.event_id)

        return (
          <div key={ev.event_id} style={{ border: '1px solid #ddd', borderRadius: 8, padding: 12, marginBottom: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
              <div>
                <strong>{ev.name}</strong>
                <div style={{ fontSize: 12, color: '#888' }}>
                  {new Date(ev.starts_at).toLocaleString()}
                </div>
              </div>
            </div>

            {mainMarkets.map(m => (
              <MarketRow key={m.market_id} market={m} onSelect={sel => handleSelect(m, sel)} />
            ))}

            {altMarkets.length > 0 && (
              <button
                onClick={() => toggleExpand(ev.event_id)}
                style={{
                  background: 'none', border: 'none', color: '#1976d2',
                  cursor: 'pointer', fontSize: 13, padding: '4px 0', marginTop: 4,
                }}
              >
                {expanded ? 'Hide Alt Lines' : `Show All Lines (${altMarkets.length})`}
              </button>
            )}

            {expanded && altMarkets.map(m => (
              <MarketRow key={m.market_id} market={m} onSelect={sel => handleSelect(m, sel)} />
            ))}
          </div>
        )
      })}
    </div>
  )
}

function MarketRow({ market, onSelect }: { market: Market; onSelect: (sel: Selection) => void }) {
  const typeLabel = market.market_type === 'ML' ? 'Moneyline'
    : market.market_type === 'SPREAD' ? 'Spread'
    : market.market_type === 'TOTAL' ? 'Total'
    : market.market_type

  return (
    <div style={{ marginBottom: 6 }}>
      <div style={{ fontSize: 12, color: '#666', marginBottom: 2 }}>
        {typeLabel}{market.target_value > 0 && market.market_type !== 'ML' ? ` ${market.target_value}` : ''}
      </div>
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
        {market.selections.map(sel => (
          <button
            key={sel.selection_id}
            onClick={() => onSelect(sel)}
            disabled={sel.odds_decimal <= 0}
            style={{
              padding: '6px 10px',
              border: '1px solid #ccc',
              borderRadius: 4,
              background: sel.odds_decimal > 0 ? '#fff' : '#f5f5f5',
              cursor: sel.odds_decimal > 0 ? 'pointer' : 'default',
              fontSize: 13,
              minWidth: 100,
              textAlign: 'center',
            }}
          >
            <div style={{ fontWeight: 500 }}>{formatLine(sel, market)}</div>
            {sel.odds_decimal > 0
              ? <div style={{ color: '#1976d2', fontWeight: 600 }}>{formatAmerican(sel.odds_american)}</div>
              : <div style={{ color: '#999' }}>N/A</div>
            }
          </button>
        ))}
      </div>
    </div>
  )
}
