import { useState } from 'react'
import EventList, { type SelectedBet } from './components/EventList'
import BetSlip from './components/BetSlip'
import MyBets from './components/MyBets'
import Login from './components/Login'
import Register from './components/Register'
import { setAuthToken } from './api'

type Tab = 'events' | 'mybets'
type AuthView = 'login' | 'register'

export default function App() {
  const [token, setToken] = useState<string | null>(null)
  const [tab, setTab] = useState<Tab>('events')
  const [authView, setAuthView] = useState<AuthView>('login')
  const [selectedBet, setSelectedBet] = useState<SelectedBet | null>(null)

  function handleAuth(t: string) {
    setAuthToken(t)
    setToken(t)
  }

  if (!token) {
    return authView === 'login'
      ? <Login onLogin={handleAuth} onSwitchToRegister={() => setAuthView('register')} />
      : <Register onRegister={handleAuth} onSwitchToLogin={() => setAuthView('login')} />
  }

  return (
    <div style={{
      minHeight: '100vh',
      background: '#07152b',
      fontFamily: "'Inter', 'Segoe UI', system-ui, sans-serif",
    }}>
    <div style={{ maxWidth: 560, margin: '0 auto', padding: '16px 12px' }}>
      <h1 style={{ fontSize: 18, fontWeight: 800, color: '#f5c518', marginBottom: 16, letterSpacing: '0.05em' }}>
        SPORTSBOOK
      </h1>

      <nav style={{ display: 'flex', gap: 4, marginBottom: 16, borderBottom: '1px solid #1c3354', paddingBottom: 0 }}>
        {(['events', 'mybets'] as Tab[]).map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            style={{
              background: 'none',
              border: 'none',
              borderBottom: tab === t ? '2px solid #f5c518' : '2px solid transparent',
              color: tab === t ? '#f5c518' : '#6b849e',
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
        <>
          <EventList onSelectBet={setSelectedBet} />
          {selectedBet && (
            <div style={{ marginTop: 16 }}>
              <BetSlip selectedBet={selectedBet} onClear={() => setSelectedBet(null)} />
            </div>
          )}
        </>
      ) : (
        <MyBets />
      )}
    </div>
    </div>
  )
}
