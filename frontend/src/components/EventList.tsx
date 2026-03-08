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

function dateKey(iso: string): string {
  // Returns YYYY-MM-DD in local time for grouping.
  const d = new Date(iso)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function dateLabel(key: string): string {
  const today = dateKey(new Date().toISOString())
  const tomorrow = dateKey(new Date(Date.now() + 86_400_000).toISOString())
  if (key === today) return 'TODAY'
  if (key === tomorrow) return 'TOMORROW'
  // e.g. "MON, MAR 10"
  const d = new Date(`${key}T12:00:00`)
  return d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' }).toUpperCase()
}

export default function EventList({ onSelectBet }: Props) {
  const [events, setEvents] = useState<Event[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [altOpen, setAltOpen] = useState<Record<string, boolean>>({})

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

  // Group events by local calendar date, preserving sort order.
  const groups: { key: string; evs: Event[] }[] = []
  for (const ev of events) {
    const k = dateKey(ev.starts_at)
    const last = groups[groups.length - 1]
    if (last && last.key === k) {
      last.evs.push(ev)
    } else {
      groups.push({ key: k, evs: [ev] })
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
      {groups.map(({ key, evs }) => {
        const isCollapsed = collapsed[key] ?? false
        return (
        <div key={key}>
          {/* Date section header */}
          <button
            onClick={() => setCollapsed(prev => ({ ...prev, [key]: !isCollapsed }))}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              width: '100%',
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              padding: '16px 0 10px',
              textAlign: 'left',
            }}
          >
            <span style={{
              fontSize: 13,
              fontWeight: 800,
              letterSpacing: '0.1em',
              color: C.text,
              flexShrink: 0,
            }}>
              {dateLabel(key)}
            </span>
            <span style={{ fontSize: 11, color: C.muted, flexShrink: 0 }}>
              {evs.length} {evs.length === 1 ? 'game' : 'games'}
            </span>
            <div style={{ flex: 1, height: 1, background: C.border }} />
            <span style={{
              fontSize: 10,
              color: C.muted,
              flexShrink: 0,
              transform: isCollapsed ? 'rotate(-90deg)' : 'rotate(0deg)',
              transition: 'transform 0.2s',
              display: 'inline-block',
            }}>▼</span>
          </button>

          {!isCollapsed && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginBottom: 6 }}>
      {evs.map(ev => {
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

        const altSpreads = ev.markets.filter(m => !m.is_main && m.market_type === 'SPREAD')
        const altTotals  = ev.markets.filter(m => !m.is_main && m.market_type === 'TOTAL')
        const hasAlts    = altSpreads.length > 0 || altTotals.length > 0
        const showAlts   = altOpen[ev.event_id] ?? false

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
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}>
              <div>
                <span style={{ fontWeight: 700, fontSize: 13, color: C.text }}>{ev.name}</span>
                <span style={{ fontSize: 11, color: C.muted, marginLeft: 10 }}>
                  {new Date(ev.starts_at).toLocaleTimeString(undefined, {
                    hour: '2-digit', minute: '2-digit',
                  })}
                </span>
              </div>
              {hasAlts && (
                <button
                  onClick={() => setAltOpen(prev => ({ ...prev, [ev.event_id]: !showAlts }))}
                  style={{
                    background: showAlts ? C.btnBg : 'transparent',
                    border: `1px solid ${showAlts ? C.btnOdds : C.border}`,
                    borderRadius: 4,
                    color: showAlts ? C.btnOdds : C.muted,
                    fontSize: 10,
                    fontWeight: 700,
                    letterSpacing: '0.08em',
                    padding: '3px 8px',
                    cursor: 'pointer',
                    whiteSpace: 'nowrap',
                  }}
                >
                  ALT LINES {showAlts ? '▲' : '▼'}
                </button>
              )}
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

            {/* Alt lines panel */}
            {showAlts && (altSpreads.length > 0 || altTotals.length > 0) && (
              <div style={{ borderTop: `1px solid ${C.border}`, background: C.bg }}>
                {[
                  { label: 'ALT SPREADS', markets: altSpreads },
                  { label: 'ALT TOTALS',  markets: altTotals  },
                ].filter(g => g.markets.length > 0).map(group => (
                  <div key={group.label}>
                    <div style={{
                      padding: '5px 14px',
                      fontSize: 10,
                      fontWeight: 800,
                      letterSpacing: '0.1em',
                      color: C.gold,
                      borderBottom: `1px solid ${C.border}`,
                      background: C.cardHeader,
                    }}>
                      {group.label}
                    </div>
                    {group.markets.map(altMarket => (
                      altMarket.selections.map((sel, si) => {
                        const disabled = sel.odds_decimal <= 0
                        const active = selectedId === sel.selection_id
                        return (
                          <div key={sel.selection_id} style={{
                            display: 'grid',
                            gridTemplateColumns: '1fr 96px',
                            borderBottom: `1px solid ${C.border}`,
                            alignItems: 'center',
                          }}>
                            <div style={{
                              padding: '8px 14px',
                              fontSize: 12,
                              color: C.muted,
                              whiteSpace: 'nowrap',
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                            }}>
                              {rows[si]?.name ?? sel.name}
                            </div>
                            <div style={{ borderLeft: `1px solid ${C.border}`, padding: '5px 7px' }}>
                              <button
                                disabled={disabled}
                                onClick={() => {
                                  if (disabled) return
                                  setSelectedId(sel.selection_id)
                                  onSelectBet({
                                    market_id: altMarket.market_id,
                                    market_name: altMarket.name,
                                    selection_id: sel.selection_id,
                                    selection_name: formatLine(sel, altMarket) || sel.name,
                                    odds_decimal: sel.odds_decimal,
                                    odds_american: sel.odds_american,
                                  })
                                }}
                                style={{
                                  width: '100%',
                                  padding: '6px 2px',
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
                                    <div style={{ fontSize: 10, color: active ? C.selText : C.muted, marginBottom: 1, fontWeight: 600 }}>
                                      {formatLine(sel, altMarket)}
                                    </div>
                                    <div style={{ fontSize: 13, fontWeight: 700, color: active ? C.selText : C.btnOdds }}>
                                      {formatAmerican(sel.odds_american)}
                                    </div>
                                  </>
                                ) : (
                                  <div style={{ fontSize: 13, color: C.border }}>—</div>
                                )}
                              </button>
                            </div>
                          </div>
                        )
                      })
                    ))}
                  </div>
                ))}
              </div>
            )}
          </div>
        )
      })}
          </div>
          )}
        </div>
        )
      })}
    </div>
  )
}
