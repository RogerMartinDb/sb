import { useState } from 'react'
import EventList, { type SelectedBet } from './components/EventList'
import BetSlip from './components/BetSlip'
import MyBets from './components/MyBets'
import Login from './components/Login'
import Register from './components/Register'
import CashierModal from './components/CashierModal'
import { setAuthToken, getBalance } from './api'
import { useIsMobile } from './hooks/useIsMobile'

type Tab = 'events' | 'mybets'
type AuthModal = 'login' | 'register' | null
export type OddsFormat = 'american' | 'decimal' | 'cent'

const C = {
  bg:        '#07152b',
  sidebar:   '#091729',
  border:    '#1c3354',
  gold:      '#f5c518',
  text:      '#e2e8f0',
  muted:     '#6b849e',
  active:    '#142a4a',
  live:      '#e74c3c',
}

interface Competition {
  id: string
  label: string
}

interface Sport {
  id: string
  label: string
  competitions: Competition[]
}

const SPORTS: Sport[] = [
  {
    id: 'basketball',
    label: 'Basketball',
    competitions: [
      { id: 'nba',   label: 'NBA'   },
      { id: 'ncaab', label: 'NCAAB' },
    ],
  },
  {
    id: 'hockey',
    label: 'Hockey',
    competitions: [
      { id: 'nhl', label: 'NHL' },
    ],
  },
  {
    id: 'politics',
    label: 'Politics',
    competitions: [
      { id: 'iran', label: 'Iran' },
    ],
  },
]


interface LeftMenuProps {
  selected: string
  onSelect: (id: string) => void
}

function LeftMenu({ selected, onSelect }: LeftMenuProps) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({ basketball: true, hockey: true, politics: true })
  const isMobile = useIsMobile()

  return (
    <div style={{
      width: isMobile ? '100%' : 160,
      flexShrink: 0,
      background: C.sidebar,
      border: `1px solid ${C.border}`,
      borderRadius: isMobile ? 0 : 10,
      overflow: 'hidden',
      alignSelf: 'flex-start',
      fontFamily: "'Inter', 'Segoe UI', system-ui, sans-serif",
    }}>
      {/* Live — top-level selectable */}
      <button
        onClick={() => onSelect('__live__')}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          width: '100%',
          background: selected === '__live__' ? '#1a0a0a' : 'none',
          border: 'none',
          borderBottom: `1px solid ${C.border}`,
          borderLeft: selected === '__live__' ? `3px solid ${C.live}` : '3px solid transparent',
          color: C.live,
          fontSize: 12,
          fontWeight: 800,
          letterSpacing: '0.08em',
          padding: '10px 14px',
          cursor: 'pointer',
          textAlign: 'left',
        }}
      >
        <span style={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          background: C.live,
          boxShadow: `0 0 5px ${C.live}`,
          flexShrink: 0,
          display: 'inline-block',
          animation: 'pulse 2s ease-in-out infinite',
        }} />
        LIVE
      </button>

      {SPORTS.map(sport => {
        const isOpen = expanded[sport.id] ?? false
        return (
          <div key={sport.id}>
            <button
              onClick={() => setExpanded(prev => ({ ...prev, [sport.id]: !isOpen }))}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                width: '100%',
                background: 'none',
                border: 'none',
                borderBottom: `1px solid ${C.border}`,
                color: C.gold,
                fontSize: 12,
                fontWeight: 800,
                letterSpacing: '0.08em',
                padding: '10px 14px',
                cursor: 'pointer',
                textAlign: 'left',
              }}
            >
              {sport.label}
              <span style={{
                fontSize: 10,
                color: C.muted,
                transform: isOpen ? 'rotate(0deg)' : 'rotate(-90deg)',
                transition: 'transform 0.2s',
                display: 'inline-block',
              }}>▼</span>
            </button>

            {isOpen && sport.competitions.map(comp => {
              const isActive = selected === comp.id
              return (
                <button
                  key={comp.id}
                  onClick={() => onSelect(comp.id)}
                  style={{
                    display: 'block',
                    width: '100%',
                    background: isActive ? C.active : 'none',
                    border: 'none',
                    borderBottom: `1px solid ${C.border}`,
                    borderLeft: isActive ? `3px solid ${C.gold}` : '3px solid transparent',
                    color: isActive ? C.text : C.muted,
                    fontSize: 12,
                    fontWeight: isActive ? 700 : 400,
                    padding: '8px 14px 8px 16px',
                    cursor: 'pointer',
                    textAlign: 'left',
                  }}
                >
                  {comp.label}
                </button>
              )
            })}
          </div>
        )
      })}
    </div>
  )
}

const btnBase: React.CSSProperties = {
  border: 'none',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 12,
  fontWeight: 700,
  letterSpacing: '0.06em',
  padding: '6px 14px',
}

export default function App() {
  const [token, setToken] = useState<string | null>(null)
  const [email, setEmail] = useState<string | null>(null)
  const [tab, setTab] = useState<Tab>('events')
  const [authModal, setAuthModal] = useState<AuthModal>(null)
  const [selectedBet, setSelectedBet] = useState<SelectedBet | null>(null)
  const [competitionFilter, setCompetitionFilter] = useState<string>('nba')
  const [menuOpen, setMenuOpen] = useState(false)
  const [oddsFormat, setOddsFormat] = useState<OddsFormat>('american')
  const [balance, setBalance] = useState<number | null>(null)
  const [cashierModal, setCashierModal] = useState<'deposit' | 'withdraw' | null>(null)
  const [accountMenuOpen, setAccountMenuOpen] = useState(false)
  const isMobile = useIsMobile()

  async function handleAuth(t: string, e: string) {
    setAuthToken(t)
    setToken(t)
    setEmail(e)
    setAuthModal(null)
    try {
      const bal = await getBalance(t)
      setBalance(bal.available_minor)
    } catch {
      // balance will remain null if cashier is not running
    }
  }

  function handleLogout() {
    setAuthToken(null)
    setToken(null)
    setEmail(null)
    setBalance(null)
    setTab('events')
  }

  return (
    <div style={{
      minHeight: '100vh',
      background: C.bg,
      fontFamily: "'Inter', 'Segoe UI', system-ui, sans-serif",
    }}>
      {authModal && (
        <div
          onClick={() => setAuthModal(null)}
          style={{
            position: 'fixed', inset: 0,
            background: 'rgba(0,0,0,0.6)',
            zIndex: 100,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <div onClick={e => e.stopPropagation()}>
            {authModal === 'login'
              ? <Login
                  onLogin={handleAuth}
                  onSwitchToRegister={() => setAuthModal('register')}
                />
              : <Register
                  onRegister={handleAuth}
                  onSwitchToLogin={() => setAuthModal('login')}
                />
            }
          </div>
        </div>
      )}

      {cashierModal && token && (
        <CashierModal
          mode={cashierModal}
          token={token}
          onClose={() => setCashierModal(null)}
          onSuccess={(newBalanceMinor) => {
            setBalance(newBalanceMinor)
            setCashierModal(null)
          }}
        />
      )}

      <div style={{ maxWidth: 900, margin: '0 auto', padding: '16px 12px' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {isMobile && tab === 'events' && (
              <button
                onClick={() => setMenuOpen(o => !o)}
                aria-label="Toggle navigation"
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  padding: 4,
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 5,
                  justifyContent: 'center',
                }}
              >
                {[0, 1, 2].map(i => (
                  <span key={i} style={{
                    display: 'block',
                    width: 22,
                    height: 2,
                    background: C.gold,
                    borderRadius: 2,
                  }} />
                ))}
              </button>
            )}
            <h1 style={{ fontSize: 18, fontWeight: 800, color: C.gold, margin: 0, letterSpacing: '0.05em' }}>
              Sports++
            </h1>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {token && email ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                <button
                  onClick={() => { setCashierModal('deposit') }}
                  style={{ ...btnBase, background: C.gold, color: '#07152b' }}
                >
                  DEPOSIT
                </button>
                <div style={{ position: 'relative' }}>
                <button
                  onClick={() => setAccountMenuOpen(o => !o)}
                  style={{ ...btnBase, background: C.active, color: C.text, display: 'flex', alignItems: 'center', gap: 7 }}
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" style={{ flexShrink: 0 }}>
                    <circle cx="8" cy="5.5" r="3" fill={C.text} />
                    <path d="M1.5 14.5c0-3.314 2.91-6 6.5-6s6.5 2.686 6.5 6" stroke={C.text} strokeWidth="1.5" strokeLinecap="round" fill="none" />
                  </svg>
                  {balance !== null ? `$${(balance / 100).toFixed(2)}` : ''} ▾
                </button>
                {accountMenuOpen && (
                  <div style={{
                    position: 'absolute', right: 0, top: '100%', marginTop: 4,
                    background: C.sidebar, border: `1px solid ${C.border}`, borderRadius: 6,
                    zIndex: 500, minWidth: 140, overflow: 'hidden',
                  }}>
                    <button
                      onClick={() => { setAccountMenuOpen(false); setCashierModal('deposit') }}
                      style={{
                        display: 'block', width: '100%', textAlign: 'left',
                        background: 'none', border: 'none', borderBottom: `1px solid ${C.border}`,
                        color: C.text, fontSize: 13, padding: '10px 14px', cursor: 'pointer',
                      }}
                    >
                      Deposit
                    </button>
                    <button
                      onClick={() => { setAccountMenuOpen(false); setCashierModal('withdraw') }}
                      style={{
                        display: 'block', width: '100%', textAlign: 'left',
                        background: 'none', border: 'none', borderBottom: `1px solid ${C.border}`,
                        color: C.text, fontSize: 13, padding: '10px 14px', cursor: 'pointer',
                      }}
                    >
                      Withdraw
                    </button>
                    <button
                      onClick={() => { setAccountMenuOpen(false); handleLogout() }}
                      style={{
                        display: 'block', width: '100%', textAlign: 'left',
                        background: 'none', border: 'none',
                        color: C.muted, fontSize: 13, padding: '10px 14px', cursor: 'pointer',
                      }}
                    >
                      Logout
                    </button>
                  </div>
                )}
              </div>
              </div>
            ) : (
              <>
                <button
                  onClick={() => setAuthModal('login')}
                  style={{ ...btnBase, background: C.active, color: C.text }}
                >
                  LOGIN
                </button>
                <button
                  onClick={() => setAuthModal('register')}
                  style={{ ...btnBase, background: C.gold, color: '#07152b' }}
                >
                  REGISTER
                </button>
              </>
            )}
          </div>
        </div>

        <nav style={{ display: 'flex', gap: 4, marginBottom: 16, borderBottom: `1px solid ${C.border}`, paddingBottom: 0, alignItems: 'center' }}>
          {(['events', 'mybets'] as Tab[]).map(t => (
            <button
              key={t}
              onClick={() => setTab(t)}
              style={{
                background: 'none',
                border: 'none',
                borderBottom: tab === t ? `2px solid ${C.gold}` : '2px solid transparent',
                color: tab === t ? C.gold : C.muted,
                cursor: 'pointer',
                fontWeight: 700,
                fontSize: 13,
                letterSpacing: '0.06em',
                padding: '6px 14px',
                marginBottom: -1,
              }}
            >
              {t === 'events' ? 'EVENTS' : 'MY BETS'}
            </button>
          ))}
          <div style={{ flex: 1 }} />
          <select
            value={oddsFormat}
            onChange={e => setOddsFormat(e.target.value as OddsFormat)}
            style={{
              background: C.active,
              color: C.muted,
              border: `1px solid ${C.border}`,
              borderRadius: 4,
              fontSize: 11,
              fontWeight: 600,
              padding: '3px 6px',
              cursor: 'pointer',
              outline: 'none',
              marginBottom: 4,
            }}
          >
            <option value="american">American</option>
            <option value="decimal">Decimal</option>
            <option value="cent">Cent</option>
          </select>
        </nav>

        {tab === 'events' ? (
          <div style={{ display: 'flex', gap: 14, alignItems: 'flex-start' }}>
            {/* Mobile drawer overlay */}
            {isMobile && menuOpen && (
              <div
                onClick={() => setMenuOpen(false)}
                style={{
                  position: 'fixed', inset: 0,
                  background: 'rgba(0,0,0,0.55)',
                  zIndex: 200,
                }}
              />
            )}
            {/* Left menu — sidebar on desktop, slide-in drawer on mobile */}
            <div style={isMobile ? {
              position: 'fixed',
              top: 0,
              left: menuOpen ? 0 : -200,
              width: 200,
              height: '100%',
              zIndex: 201,
              background: C.sidebar,
              transition: 'left 0.25s ease',
              overflowY: 'auto',
              paddingTop: 16,
            } : {}}>
              {isMobile && (
                <button
                  onClick={() => setMenuOpen(false)}
                  aria-label="Close menu"
                  style={{
                    display: 'block',
                    marginLeft: 'auto',
                    marginRight: 12,
                    marginBottom: 8,
                    background: 'none',
                    border: 'none',
                    color: C.muted,
                    fontSize: 20,
                    cursor: 'pointer',
                    lineHeight: 1,
                  }}
                >✕</button>
              )}
              <LeftMenu
                selected={competitionFilter}
                onSelect={(id) => {
                  setCompetitionFilter(id)
                  setMenuOpen(false)
                }}
              />
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <EventList
                onSelectBet={setSelectedBet}
                competitionId={competitionFilter}
                groupByDate={competitionFilter !== '__live__' && !SPORTS.find(s => s.id === 'politics')?.competitions.some(c => c.id === competitionFilter)}
                oddsFormat={oddsFormat}
              />
              {selectedBet && (
                <div style={{ marginTop: 16 }}>
                  <BetSlip selectedBet={selectedBet} onClear={() => setSelectedBet(null)} oddsFormat={oddsFormat} />
                </div>
              )}
            </div>
          </div>
        ) : (
          <MyBets oddsFormat={oddsFormat} />
        )}
      </div>
    </div>
  )
}
