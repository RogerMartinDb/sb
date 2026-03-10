import { useState, useEffect } from 'react'
import { type Event, type Market, type Selection } from '../api'
import { useEventsWS, type ScoreFlashSide } from '../hooks/useEventsWS'
import { useIsMobile } from '../hooks/useIsMobile'
import { getTeamMeta, formatTeamName, formatTeamNameShort } from '../teamMeta'

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
  competitionId?: string | null
  groupByDate?: boolean
}

function shouldFlashScore(side: ScoreFlashSide | undefined, team: 'home' | 'away'): boolean {
  if (!side) return false
  return side === 'both' || side === team
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
  live:          '#e74c3c',
  liveGlow:      'rgba(231, 76, 60, 0.25)',
}

function TeamIcon({ name, competitionId }: { name: string; competitionId: string }) {
  const meta = getTeamMeta(name, competitionId)
  if (!meta) return null
  return (
    <img
      src={meta.logo}
      alt=""
      width={18}
      height={18}
      style={{ flexShrink: 0, objectFit: 'contain' }}
      onError={e => { (e.target as HTMLImageElement).style.display = 'none' }}
    />
  )
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

export default function EventList({ onSelectBet, competitionId, groupByDate = true }: Props) {
  const { events, loading, oddsFlash, scoreFlash } = useEventsWS()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [altOpen, setAltOpen] = useState<Record<string, boolean>>({})
  const isMobile = useIsMobile()

  useEffect(() => {
    // Inject pulse animation for live indicator.
    const id = 'sb-keyframes'
    if (!document.getElementById(id)) {
      const style = document.createElement('style')
      style.id = id
      style.textContent = [
        `@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }`,
        `@keyframes flashOddsUp { 0% { background: #1b5e32; box-shadow: inset 0 0 0 1px #27ae60; } 100% { background: #142a4a; box-shadow: none; } }`,
        `@keyframes flashOddsDown { 0% { background: #5e1b1b; box-shadow: inset 0 0 0 1px #e74c3c; } 100% { background: #142a4a; box-shadow: none; } }`,
        `@keyframes flashScore { 0% { color: #f5c518; transform: scale(1.25); } 60% { color: #f5c518; transform: scale(1.1); } 100% { color: #e2e8f0; transform: scale(1); } }`,
      ].join(' ')
      document.head.appendChild(style)
    }
  }, [])

  const BASKETBALL_IDS = new Set(['nba', 'ncaab'])
  const LIVE_ALL = competitionId === '__live__'

  const visibleEvents = (
    LIVE_ALL
      ? events.filter(ev => ev.status === 'LIVE')
      : competitionId
        ? events.filter(ev => ev.competition_id === competitionId)
        : events
  ).filter(ev => {
    if (!BASKETBALL_IDS.has(ev.competition_id)) return true
    const mainMarkets = ev.markets.filter(m => m.is_main)
    return mainMarkets.some(m => m.selections.some(s => s.odds_decimal > 0))
  })

  if (loading) return <p style={{ color: C.muted, padding: 12 }}>Loading events…</p>
  if (visibleEvents.length === 0) return (
    <p style={{ color: C.muted, padding: 12 }}>
      {LIVE_ALL ? 'No live events right now.' : 'No upcoming events.'}
    </p>
  )

  const COLS: { label: string; type: string }[] = [
    { label: 'Spread', type: 'SPREAD' },
    { label: 'Moneyline',     type: 'ML' },
    { label: 'Total',  type: 'TOTAL' },
  ]

  const groups: { key: string; label: string; evs: Event[] }[] = []

  if (LIVE_ALL) {
    // Group all live events by competition, preserving insertion order.
    const byComp = new Map<string, Event[]>()
    for (const ev of visibleEvents) {
      const arr = byComp.get(ev.competition_id)
      if (arr) {
        arr.push(ev)
      } else {
        byComp.set(ev.competition_id, [ev])
      }
    }
    for (const [compId, evs] of byComp) {
      groups.push({ key: compId, label: compId.toUpperCase(), evs })
    }
  } else {
    // Separate live games from scheduled, then group scheduled by date (unless groupByDate is false).
    const liveEvents = visibleEvents.filter(ev => ev.status === 'LIVE')
    const scheduledEvents = visibleEvents.filter(ev => ev.status !== 'LIVE')

    if (liveEvents.length > 0) {
      groups.push({ key: '_live', label: 'LIVE', evs: liveEvents })
    }
    if (groupByDate) {
      for (const ev of scheduledEvents) {
        const k = dateKey(ev.starts_at)
        const last = groups[groups.length - 1]
        if (last && last.key === k) {
          last.evs.push(ev)
        } else {
          groups.push({ key: k, label: dateLabel(k), evs: [ev] })
        }
      }
    } else {
      if (scheduledEvents.length > 0) {
        groups.push({ key: '_all', label: '', evs: scheduledEvents })
      }
    }
  }

  function isBinaryEvent(ev: Event): boolean {
    return ev.markets.length > 0 && ev.markets.every(m => m.market_type === 'BINARY')
  }

  function renderBinaryEvent(ev: Event) {
    // Sort markets by closest deadline first (closes_at approximated by starts_at).
    const markets = [...ev.markets].sort((a, b) => {
      const aMain = a.is_main ? 0 : 1
      const bMain = b.is_main ? 0 : 1
      return aMain - bMain
    })

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
          padding: '10px 14px',
          borderBottom: `1px solid ${C.border}`,
        }}>
          <span style={{ fontWeight: 700, fontSize: 13, color: C.text }}>{ev.name}</span>
        </div>

        {/* One row per market */}
        {markets.map((market, mi) => {
          const yesSel = market.selections.find(s => s.name === 'Yes')
          const noSel = market.selections.find(s => s.name === 'No')
          if (!yesSel || !noSel) return null

          const yesDisabled = yesSel.odds_decimal <= 0
          const noDisabled = noSel.odds_decimal <= 0
          const yesActive = selectedId === yesSel.selection_id
          const noActive = selectedId === noSel.selection_id

          // Extract deadline from market name or use market name as-is.
          const label = market.name

          return (
            <div key={market.market_id} style={{
              borderBottom: mi < markets.length - 1 ? `1px solid ${C.border}` : 'none',
              padding: '8px 14px',
              display: 'grid',
              gridTemplateColumns: '1fr 80px 80px',
              alignItems: 'center',
              gap: 8,
            }}>
              <span style={{
                fontSize: 12,
                color: C.muted,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}>
                {label}
              </span>
              {/* Yes button */}
              <button
                disabled={yesDisabled}
                onClick={() => {
                  setSelectedId(yesSel.selection_id)
                  onSelectBet({
                    market_id: market.market_id,
                    market_name: market.name,
                    selection_id: yesSel.selection_id,
                    selection_name: 'Yes',
                    odds_decimal: yesSel.odds_decimal,
                    odds_american: yesSel.odds_american,
                  })
                }}
                style={{
                  padding: '6px 4px',
                  border: yesActive
                    ? `1.5px solid ${C.gold}`
                    : `1px solid ${yesDisabled ? 'transparent' : C.btnBorder}`,
                  borderRadius: 6,
                  background: yesActive ? C.selBg : (yesDisabled ? 'transparent' : C.btnBg),
                  cursor: yesDisabled ? 'default' : 'pointer',
                  textAlign: 'center',
                  animation: (!yesActive && !yesDisabled && oddsFlash.has(yesSel.selection_id))
                    ? `flashOdds${oddsFlash.get(yesSel.selection_id) === 'up' ? 'Up' : 'Down'} 1.5s ease-out forwards`
                    : undefined,
                }}
              >
                {!yesDisabled ? (
                  <>
                    <div style={{ fontSize: 9, color: yesActive ? C.selText : '#27ae60', fontWeight: 700, marginBottom: 1 }}>YES</div>
                    <div style={{ fontSize: 12, fontWeight: 700, color: yesActive ? C.selText : C.btnOdds }}>
                      {formatAmerican(yesSel.odds_american)}
                    </div>
                  </>
                ) : (
                  <div style={{ fontSize: 12, color: C.border }}>—</div>
                )}
              </button>
              {/* No button */}
              <button
                disabled={noDisabled}
                onClick={() => {
                  setSelectedId(noSel.selection_id)
                  onSelectBet({
                    market_id: market.market_id,
                    market_name: market.name,
                    selection_id: noSel.selection_id,
                    selection_name: 'No',
                    odds_decimal: noSel.odds_decimal,
                    odds_american: noSel.odds_american,
                  })
                }}
                style={{
                  padding: '6px 4px',
                  border: noActive
                    ? `1.5px solid ${C.gold}`
                    : `1px solid ${noDisabled ? 'transparent' : C.btnBorder}`,
                  borderRadius: 6,
                  background: noActive ? C.selBg : (noDisabled ? 'transparent' : C.btnBg),
                  cursor: noDisabled ? 'default' : 'pointer',
                  textAlign: 'center',
                  animation: (!noActive && !noDisabled && oddsFlash.has(noSel.selection_id))
                    ? `flashOdds${oddsFlash.get(noSel.selection_id) === 'up' ? 'Up' : 'Down'} 1.5s ease-out forwards`
                    : undefined,
                }}
              >
                {!noDisabled ? (
                  <>
                    <div style={{ fontSize: 9, color: noActive ? C.selText : C.live, fontWeight: 700, marginBottom: 1 }}>NO</div>
                    <div style={{ fontSize: 12, fontWeight: 700, color: noActive ? C.selText : C.btnOdds }}>
                      {formatAmerican(noSel.odds_american)}
                    </div>
                  </>
                ) : (
                  <div style={{ fontSize: 12, color: C.border }}>—</div>
                )}
              </button>
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
      {groups.map(({ key, label, evs }) => {
        const isCollapsed = collapsed[key] ?? false
        const isLiveGroup = key === '_live'
        const isUngrouped = key === '_all'
        return (
        <div key={key}>
          {/* Section header */}
          {isUngrouped ? null : <button
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
            {isLiveGroup && (
              <span style={{
                width: 8,
                height: 8,
                borderRadius: '50%',
                background: C.live,
                boxShadow: `0 0 6px ${C.live}`,
                flexShrink: 0,
                animation: 'pulse 2s ease-in-out infinite',
              }} />
            )}
            <span style={{
              fontSize: 13,
              fontWeight: 800,
              letterSpacing: '0.1em',
              color: isLiveGroup ? C.live : C.text,
              flexShrink: 0,
            }}>
              {label}
            </span>
            <span style={{ fontSize: 11, color: C.muted, flexShrink: 0 }}>
              {evs.length} {evs.length === 1 ? 'event' : 'events'}
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
          </button>}

          {(!isCollapsed || isUngrouped) && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginBottom: 6 }}>
      {evs.map(ev => {
        if (isBinaryEvent(ev)) return renderBinaryEvent(ev)

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

        const colW = isMobile ? '72px' : '96px'
        const gridCols = `1fr ${activeCols.map(() => colW).join(' ')}`

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
              background: ev.status === 'LIVE' ? `linear-gradient(135deg, ${C.cardHeader}, ${C.liveGlow})` : C.cardHeader,
              padding: '8px 14px',
              borderBottom: `1px solid ${ev.status === 'LIVE' ? C.live + '44' : C.border}`,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, flex: 1, minWidth: 0 }}>
                {ev.status === 'LIVE' && (
                  <span style={{
                    fontSize: 9,
                    fontWeight: 800,
                    letterSpacing: '0.05em',
                    color: '#fff',
                    background: C.live,
                    padding: '2px 6px',
                    borderRadius: 3,
                    flexShrink: 0,
                  }}>LIVE</span>
                )}
                <span style={{ fontWeight: 700, fontSize: 13, color: C.text, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{ev.name}</span>
                {ev.status === 'LIVE' && ev.game_period ? (
                  <span style={{ fontSize: 11, color: C.live, flexShrink: 0, fontWeight: 600 }}>
                    {ev.game_period}{ev.game_clock ? ` ${ev.game_clock}` : ''}
                  </span>
                ) : (
                  <span style={{ fontSize: 11, color: C.muted, flexShrink: 0 }}>
                    {new Date(ev.starts_at).toLocaleTimeString(undefined, {
                      hour: '2-digit', minute: '2-digit',
                    })}
                  </span>
                )}
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
                {/* Team name + score */}
                <div style={{
                  padding: isMobile ? '7px 8px' : '10px 14px',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: isMobile ? 4 : 8,
                }}>
                  <span style={{
                    fontSize: isMobile ? 11 : 13,
                    fontWeight: 600,
                    color: C.text,
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                  }}>
                    {!isMobile && <TeamIcon name={teamSel.name} competitionId={ev.competition_id} />}
                    {isMobile
                      ? formatTeamNameShort(teamSel.name, ev.competition_id)
                      : formatTeamName(teamSel.name, ev.competition_id)}
                  </span>
                  {ev.status === 'LIVE' && (
                    <span
                      key={i === 0 ? `away-${ev.away_score}` : `home-${ev.home_score}`}
                      style={{
                        fontSize: isMobile ? 12 : 15,
                        fontWeight: 800,
                        color: C.text,
                        fontVariantNumeric: 'tabular-nums',
                        flexShrink: 0,
                        display: 'inline-block',
                        animation: shouldFlashScore(scoreFlash.get(ev.event_id), i === 0 ? 'away' : 'home')
                          ? 'flashScore 1.5s ease-out forwards'
                          : undefined,
                      }}
                    >
                      {i === 0 ? ev.away_score : ev.home_score}
                    </span>
                  )}
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
                      padding: isMobile ? '4px 4px' : '6px 7px',
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
                          padding: isMobile ? '5px 2px' : '7px 2px',
                          border: active
                            ? `1.5px solid ${C.gold}`
                            : `1px solid ${disabled ? 'transparent' : C.btnBorder}`,
                          borderRadius: 6,
                          background: active ? C.selBg : (disabled ? 'transparent' : C.btnBg),
                          cursor: disabled ? 'default' : 'pointer',
                          textAlign: 'center',
                          transition: active || disabled ? 'background 0.15s' : undefined,
                          animation: (!active && !disabled && sel && oddsFlash.has(sel.selection_id))
                            ? `flashOdds${oddsFlash.get(sel.selection_id) === 'up' ? 'Up' : 'Down'} 1.5s ease-out forwards`
                            : undefined,
                        }}
                      >
                        {!disabled ? (
                          <>
                            {col.type !== 'ML' && (
                              <div style={{
                                fontSize: isMobile ? 9 : 10,
                                color: active ? C.selText : C.muted,
                                marginBottom: 1,
                                fontWeight: 600,
                              }}>
                                {formatLine(sel, market)}
                              </div>
                            )}
                            <div style={{
                              fontSize: isMobile ? 11 : 13,
                              fontWeight: 700,
                              color: active ? C.selText : C.btnOdds,
                            }}>
                              {formatAmerican(sel.odds_american)}
                            </div>
                          </>
                        ) : (
                          <div style={{ fontSize: isMobile ? 11 : 13, color: C.border }}>—</div>
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
                              padding: isMobile ? '6px 8px' : '8px 14px',
                              fontSize: isMobile ? 11 : 12,
                              color: C.muted,
                              whiteSpace: 'nowrap',
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                              display: 'flex',
                              alignItems: 'center',
                              gap: 6,
                            }}>
                              {!isMobile && <TeamIcon name={rows[si]?.name ?? sel.name} competitionId={ev.competition_id} />}
                              {isMobile
                                ? formatTeamNameShort(rows[si]?.name ?? sel.name, ev.competition_id)
                                : formatTeamName(rows[si]?.name ?? sel.name, ev.competition_id)}
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
