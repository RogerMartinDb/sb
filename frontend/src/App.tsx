import { useState } from 'react'
import EventList, { type SelectedBet } from './components/EventList'
import BetSlip from './components/BetSlip'
import MyBets from './components/MyBets'
import Login from './components/Login'
import Register from './components/Register'
import { setAuthToken } from './api'

type Tab = 'events' | 'mybets'
type AuthModal = 'login' | 'register' | null

const C = {
  bg:        '#07152b',
  sidebar:   '#091729',
  border:    '#1c3354',
  gold:      '#f5c518',
  text:      '#e2e8f0',
  muted:     '#6b849e',
  active:    '#142a4a',
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
    id: 'politics',
    label: 'Politics',
    competitions: [
      { id: 'iran', label: 'Iran' },
    ],
  },
]

interface LeftMenuProps {
  selected: string | null
  onSelect: (id: string | null) => void
}

function LeftMenu({ selected, onSelect }: LeftMenuProps) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({ basketball: true, politics: true })

  return (
    <div style={{
      width: 160,
      flexShrink: 0,
      background: C.sidebar,
      border: `1px solid ${C.border}`,
      borderRadius: 10,
      overflow: 'hidden',
      alignSelf: 'flex-start',
      fontFamily: "'Inter', 'Segoe UI', system-ui, sans-serif",
    }}>
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
                  onClick={() => onSelect(isActive ? null : comp.id)}
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
  const [competitionFilter, setCompetitionFilter] = useState<string | null>(null)

  function handleAuth(t: string, e: string) {
    setAuthToken(t)
    setToken(t)
    setEmail(e)
    setAuthModal(null)
  }

  function handleLogout() {
    setAuthToken(null)
    setToken(null)
    setEmail(null)
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

      <div style={{ maxWidth: 900, margin: '0 auto', padding: '16px 12px' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
          <h1 style={{ fontSize: 18, fontWeight: 800, color: C.gold, margin: 0, letterSpacing: '0.05em' }}>
            SPORTSBOOK
          </h1>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {token && email ? (
              <>
                <span style={{ color: C.muted, fontSize: 13 }}>{email.split('@')[0]}</span>
                <button
                  onClick={handleLogout}
                  style={{ ...btnBase, background: C.active, color: C.text }}
                >
                  LOGOUT
                </button>
              </>
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

        <nav style={{ display: 'flex', gap: 4, marginBottom: 16, borderBottom: `1px solid ${C.border}`, paddingBottom: 0 }}>
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
        </nav>

        {tab === 'events' ? (
          <div style={{ display: 'flex', gap: 14, alignItems: 'flex-start' }}>
            <LeftMenu selected={competitionFilter} onSelect={setCompetitionFilter} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <EventList
                onSelectBet={setSelectedBet}
                competitionId={competitionFilter}
                groupByDate={!SPORTS.find(s => s.id === 'politics')?.competitions.some(c => c.id === competitionFilter)}
              />
              {selectedBet && (
                <div style={{ marginTop: 16 }}>
                  <BetSlip selectedBet={selectedBet} onClear={() => setSelectedBet(null)} />
                </div>
              )}
            </div>
          </div>
        ) : (
          <MyBets />
        )}
      </div>
    </div>
  )
}
