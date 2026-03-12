import { useState } from 'react'
import { login } from '../api'

interface Props {
  onLogin: (token: string, email: string) => void
  onSwitchToRegister: () => void
}

export default function Login({ onLogin, onSwitchToRegister }: Props) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    setLoading(true)
    try {
      const token = await login(email, password)
      onLogin(token, email)
    } catch {
      setError('Invalid credentials')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ maxWidth: 320, margin: '80px auto', fontFamily: 'sans-serif', padding: 24, border: '1px solid #ddd', borderRadius: 8, background: '#fff', color: '#000' }}>
      <h2 style={{ marginBottom: 16 }}>Sign in</h2>
      <form onSubmit={handleSubmit}>
        <div style={{ marginBottom: 12 }}>
          <label>Email<br />
            <input
              type="email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              required
              style={{ width: '100%', padding: 8, boxSizing: 'border-box' }}
            />
          </label>
        </div>
        <div style={{ marginBottom: 12 }}>
          <label>Password<br />
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              required
              style={{ width: '100%', padding: 8, boxSizing: 'border-box' }}
            />
          </label>
        </div>
        {error && <p style={{ color: 'red', fontSize: 14 }}>{error}</p>}
        <button type="submit" disabled={loading} style={{ width: '100%', padding: 10 }}>
          {loading ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      <p style={{ marginTop: 12, fontSize: 14, textAlign: 'center' }}>
        No account?{' '}
        <button onClick={onSwitchToRegister} style={{ background: 'none', border: 'none', color: '#0066cc', cursor: 'pointer', padding: 0 }}>
          Create one
        </button>
      </p>
    </div>
  )
}
