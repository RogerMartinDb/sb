import { useState } from 'react'
import { register } from '../api'

interface Props {
  onRegister: (token: string, email: string) => void
  onSwitchToLogin: () => void
}

export default function Register({ onRegister, onSwitchToLogin }: Props) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (password !== confirm) {
      setError('Passwords do not match')
      return
    }
    setError(null)
    setLoading(true)
    try {
      const token = await register(email, password)
      onRegister(token, email)
    } catch {
      setError('Registration failed. Email may already be in use.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ maxWidth: 320, margin: '80px auto', fontFamily: 'sans-serif', padding: 24, border: '1px solid #ddd', borderRadius: 8 }}>
      <h2 style={{ marginBottom: 16 }}>Create account</h2>
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
              minLength={8}
              style={{ width: '100%', padding: 8, boxSizing: 'border-box' }}
            />
          </label>
        </div>
        <div style={{ marginBottom: 12 }}>
          <label>Confirm password<br />
            <input
              type="password"
              value={confirm}
              onChange={e => setConfirm(e.target.value)}
              required
              style={{ width: '100%', padding: 8, boxSizing: 'border-box' }}
            />
          </label>
        </div>
        {error && <p style={{ color: 'red', fontSize: 14 }}>{error}</p>}
        <button type="submit" disabled={loading} style={{ width: '100%', padding: 10 }}>
          {loading ? 'Creating account…' : 'Register'}
        </button>
      </form>
      <p style={{ marginTop: 12, fontSize: 14, textAlign: 'center' }}>
        Already have an account?{' '}
        <button onClick={onSwitchToLogin} style={{ background: 'none', border: 'none', color: '#0066cc', cursor: 'pointer', padding: 0 }}>
          Sign in
        </button>
      </p>
    </div>
  )
}
