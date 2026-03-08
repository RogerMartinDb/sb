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
      return `${sign}${sel.target_value}`
    }
    case 'TOTAL':
      return sel.target_value > 0 ? `O ${Math.abs(sel.target_value)}` : `U ${Math.abs(sel.target_value)}`
    default:
      return ''
  }
}

const C = {
  bg:            '#07152b',
  card:          '#0d1f3c',
  cardHeader:    '#091729',
  border:        '#1c3354',
  gold:          '#f5c518',
  text:          '#e2e8f0',
  muted:         '#6b849e',
  btnBg:         '#142a4a',
  btnBorder:     '#1e3a5f',
  btnOdds:       '#5dade2',
  selBg:         '#f5c518',
  selText:       '#071222',
}

export default function EventList({ onSelectBet }: Props) {
  const [events, setEvents] = useState<Event[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedId, setSelectedId] = useState<string | null>(null)

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

  if (loading) return <p style={{ color: C.muted, padding: 12 }}>Loading events…</p>
  if (events.length === 0) return <p style={{ color: C.muted, padding: 12 }}>No upcoming events.</p>

  const COLS: { label: string; type: string }[] = [
    { label: 'Spread', type: 'SPREAD' },
    { label: 'Moneyline',     type: 'ML' },
    { label: 'Total',  type: 'TOTAL' },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      {events.map(ev => {
        const mainMarkets = ev.markets.filter(m => m.is_main)
        const byType = Object.fromEntries(
          COLS.map(c => [c.type, mainMarkets.find((m: Market) => m.market_type === c.type)])
        )

        // derive team rows from whichever main market has the most selections
        const baseMarket = byType['ML'] ?? byType['SPREAD'] ?? mainMarkets[0]
        if (!baseMarket) return null
        const rows: Selection[] = baseMarket.selections

        // active columns (only those with data)
        const activeCols = COLS.filter(c => byType[c.type])

        const gridCols = `1fr ${activeCols.map(() => '96px').join(' ')}`

        return (
          <div key={ev.event_id} style={{
            background: C.card,
            border: `1px solid ${C.border}`,
            borderRadius: 10,
            overflow: 'hidden',
            fontFamily: "'Inter', 'Segoe UI', system-ui, sans-serif",
          }}>
            {/* Event header */}
            <div style={{
              background: C.cardHeader,
              padding: '8px 14px',
              borderBottom: `1px solid ${C.border}`,
            }}>
              <span style={{ fontWeight: 700, fontSize: 13, color: C.text }}>{ev.name}</span>
              <span style={{ fontSize: 11, color: C.muted, marginLeft: 10 }}>
                {new Date(ev.starts_at).toLocaleString(undefined, {
                  month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
                })}
              </span>
            </div>

            {/* Column headers */}
            <div style={{
              display: 'grid',
              gridTemplateColumns: gridCols,
              borderBottom: `1px solid ${C.border}`,
              background: C.cardHeader,
            }}>
              <div />
              {activeCols.map(col => (
                <div key={col.label} style={{
                  textAlign: 'center',
                  padding: '5px 4px',
                  fontSize: 10,
                  fontWeight: 800,
                  letterSpacing: '0.1em',
                  color: C.gold,
                  borderLeft: `1px solid ${C.border}`,
                }}>
                  {col.label}
                </div>
              ))}
            </div>

            {/* One row per team/selection */}
            {rows.map((teamSel, i) => (
              <div key={teamSel.selection_id} style={{
                display: 'grid',
                gridTemplateColumns: gridCols,
                borderBottom: i < rows.length - 1 ? `1px solid ${C.border}` : 'none',
                alignItems: 'center',
              }}>
                {/* Team name */}
                <div style={{
                  padding: '10px 14px',
                  fontSize: 13,
                  fontWeight: 600,
                  color: C.text,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}>
                  {teamSel.name}
                </div>

                {/* Odds button per column */}
                {activeCols.map(col => {
                  const market = byType[col.type]!
                  const sel = market.selections[i]
                  const disabled = !sel || sel.odds_decimal <= 0
                  const active = sel && selectedId === sel.selection_id

                  return (
                    <div key={col.label} style={{
                      borderLeft: `1px solid ${C.border}`,
                      padding: '6px 7px',
                    }}>
                      <button
                        disabled={disabled}
                        onClick={() => {
                          if (!sel) return
                          setSelectedId(sel.selection_id)
                          onSelectBet({
                            market_id: market.market_id,
                            market_name: market.name,
                            selection_id: sel.selection_id,
                            selection_name: formatLine(sel, market) || sel.name,
                            odds_decimal: sel.odds_decimal,
                            odds_american: sel.odds_american,
                          })
                        }}
                        style={{
                          width: '100%',
                          padding: '7px 2px',
                          border: active
                            ? `1.5px solid ${C.gold}`
                            : `1px solid ${disabled ? 'transparent' : C.btnBorder}`,
                          borderRadius: 6,
                          background: active ? C.selBg : (disabled ? 'transparent' : C.btnBg),
                          cursor: disabled ? 'default' : 'pointer',
                          textAlign: 'center',
                          transition: 'background 0.15s',
                        }}
                      >
                        {!disabled ? (
                          <>
                            {col.type !== 'ML' && (
                              <div style={{
                                fontSize: 10,
                                color: active ? C.selText : C.muted,
                                marginBottom: 1,
                                fontWeight: 600,
                              }}>
                                {formatLine(sel, market)}
                              </div>
                            )}
                            <div style={{
                              fontSize: 13,
                              fontWeight: 700,
                              color: active ? C.selText : C.btnOdds,
                            }}>
                              {formatAmerican(sel.odds_american)}
                            </div>
                          </>
                        ) : (
                          <div style={{ fontSize: 13, color: C.border }}>—</div>
                        )}
                      </button>
                    </div>
                  )
                })}
              </div>
            ))}
          </div>
        )
      })}
    </div>
  )
}
