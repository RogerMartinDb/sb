import { useEffect, useState } from 'react'
import { getMyBets, type Bet } from '../api'
import type { OddsFormat } from '../App'

const STATUS_COLOURS: Record<string, string> = {
  PLACED:       '#1976d2',
  SETTLED_WIN:  '#4caf50',
  SETTLED_LOSS: '#f44336',
  VOID:         '#9e9e9e',
}

function formatOdds(decimal: number, american: number, format: OddsFormat = 'american'): string {
  switch (format) {
    case 'decimal': return decimal.toFixed(2)
    case 'cent':    return (100 / decimal).toFixed(1)
    default:        return american >= 0 ? `+${american}` : `${american}`
  }
}

interface Props {
  oddsFormat?: OddsFormat
}

export default function MyBets({ oddsFormat = 'american' }: Props) {
  const [bets, setBets] = useState<Bet[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const data = await getMyBets()
        if (!cancelled) setBets(data.bets)
      } catch {
        if (!cancelled) setError('Could not load bets.')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    // Poll every 5s to show async settlement.
    const interval = setInterval(load, 5000)
    return () => { cancelled = true; clearInterval(interval) }
  }, [])

  if (loading) return <p>Loading…</p>
  if (error) return <p style={{ color: 'red' }}>{error}</p>
  if (bets.length === 0) return <p style={{ color: '#888' }}>No bets placed yet.</p>

  return (
    <div>
      <h2 style={{ fontSize: 16, marginBottom: 12 }}>My Bets</h2>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
        {bets.map(bet => (
          <li
            key={bet.bet_id}
            style={{
              border: '1px solid #ddd',
              borderRadius: 8,
              padding: 12,
              marginBottom: 10,
            }}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
              <span style={{ fontSize: 13, color: '#555' }}>
                {bet.selection_id}
              </span>
              <span
                style={{
                  fontSize: 12,
                  fontWeight: 600,
                  color: STATUS_COLOURS[bet.status] ?? '#555',
                }}
              >
                {bet.status.replace('_', ' ')}
              </span>
            </div>

            <div style={{ fontSize: 13, marginBottom: 4 }}>
              Odds: <strong>{formatOdds(bet.odds_decimal, bet.odds_american, oddsFormat)}</strong>
              {' · '}
              Stake: <strong>£{(bet.stake_minor / 100).toFixed(2)}</strong>
              {bet.payout_minor != null && (
                <>{' · '}Payout: <strong>£{(bet.payout_minor / 100).toFixed(2)}</strong></>
              )}
            </div>

            <div style={{ fontSize: 11, color: '#aaa' }}>
              Placed: {new Date(bet.placed_at).toLocaleString()}
              {bet.settled_at && ` · Settled: ${new Date(bet.settled_at).toLocaleString()}`}
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}
