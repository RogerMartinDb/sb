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
    <div style={{ maxWidth: 520, margin: '0 auto', fontFamily: 'sans-serif', padding: 16 }}>
      <h1 style={{ fontSize: 20, marginBottom: 16 }}>Sportsbook</h1>

      <nav style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <button
          onClick={() => setTab('events')}
          style={{ fontWeight: tab === 'events' ? 'bold' : 'normal' }}
        >
          Events
        </button>
        <button
          onClick={() => setTab('mybets')}
          style={{ fontWeight: tab === 'mybets' ? 'bold' : 'normal' }}
        >
          My Bets
        </button>
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
  )
}
