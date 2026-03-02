import { useState } from 'react'
import BetSlip from './components/BetSlip'
import MyBets from './components/MyBets'
import Login from './components/Login'
import Register from './components/Register'
import { setAuthToken } from './api'

type Tab = 'bet' | 'mybets'
type AuthView = 'login' | 'register'

export default function App() {
  const [token, setToken] = useState<string | null>(null)
  const [tab, setTab] = useState<Tab>('bet')
  const [authView, setAuthView] = useState<AuthView>('login')

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
    <div style={{ maxWidth: 480, margin: '0 auto', fontFamily: 'sans-serif', padding: 16 }}>
      <h1 style={{ fontSize: 20, marginBottom: 16 }}>Sportsbook</h1>

      <nav style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <button
          onClick={() => setTab('bet')}
          style={{ fontWeight: tab === 'bet' ? 'bold' : 'normal' }}
        >
          Place Bet
        </button>
        <button
          onClick={() => setTab('mybets')}
          style={{ fontWeight: tab === 'mybets' ? 'bold' : 'normal' }}
        >
          My Bets
        </button>
      </nav>

      {tab === 'bet' ? <BetSlip /> : <MyBets />}
    </div>
  )
}
