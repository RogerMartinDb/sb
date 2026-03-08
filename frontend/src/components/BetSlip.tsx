import { useState } from 'react'
import { placeBet, ApiError, type PlaceBetResponse } from '../api'
import type { SelectedBet } from './EventList'

interface Props {
  selectedBet: SelectedBet
  onClear: () => void
}

function formatOdds(decimal: number, american: number) {
  const americanStr = american >= 0 ? `+${american}` : `${american}`
  return `${decimal.toFixed(2)} (${americanStr})`
}

export default function BetSlip({ selectedBet, onClear }: Props) {
  const [stake, setStake] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<PlaceBetResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function handlePlaceBet() {
    const stakeMinor = Math.round(parseFloat(stake) * 100)
    if (isNaN(stakeMinor) || stakeMinor <= 0) {
      setError('Enter a valid stake')
      return
    }

    setError(null)
    setResult(null)
    setLoading(true)

    try {
      const resp = await placeBet({
        market_id: selectedBet.market_id,
        selection_id: selectedBet.selection_id,
        odds_decimal: selectedBet.odds_decimal,
        odds_american: selectedBet.odds_american,
        stake_minor: stakeMinor,
        currency: 'GBP',
      }, crypto.randomUUID())
      setResult(resp)
    } catch (err) {
      if (err instanceof ApiError) {
        setError(friendlyError(err.code))
      } else {
        setError('Something went wrong. Please try again.')
      }
    } finally {
      setLoading(false)
    }
  }

  if (result) {
    return (
      <div style={{ border: '1px solid #4caf50', borderRadius: 8, padding: 16 }}>
        <h3 style={{ color: '#4caf50' }}>Bet Accepted</h3>
        <p>Bet ID: <code>{result.bet_id}</code></p>
        <p>Odds: {formatOdds(result.odds_decimal, result.odds_american)}</p>
        <p>Stake: £{(result.stake / 100).toFixed(2)}</p>
        <button onClick={() => { setResult(null); setStake(''); onClear() }}>
          Place Another Bet
        </button>
      </div>
    )
  }

  return (
    <div style={{ border: '1px solid #ddd', borderRadius: 8, padding: 16 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <h3 style={{ fontSize: 15, margin: 0 }}>Bet Slip</h3>
        <button onClick={onClear} style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#999' }}>✕</button>
      </div>

      <p style={{ fontSize: 14, marginBottom: 4 }}>{selectedBet.market_name}</p>
      <p style={{ fontSize: 14, fontWeight: 600, marginBottom: 12 }}>
        {selectedBet.selection_name} @ {formatOdds(selectedBet.odds_decimal, selectedBet.odds_american)}
      </p>

      <div style={{ marginBottom: 12 }}>
        <label>
          Stake (£)
          <br />
          <input
            type="number"
            min="0.01"
            step="0.01"
            value={stake}
            onChange={e => setStake(e.target.value)}
            style={{ padding: 8, width: 120, marginTop: 4 }}
            placeholder="0.00"
          />
        </label>
      </div>

      {stake && !isNaN(parseFloat(stake)) && parseFloat(stake) > 0 && (
        <p style={{ fontSize: 13, color: '#555' }}>
          Potential return: £{(parseFloat(stake) * selectedBet.odds_decimal).toFixed(2)}
        </p>
      )}

      {error && <p style={{ color: 'red', fontSize: 14 }}>{error}</p>}

      <button
        onClick={handlePlaceBet}
        disabled={loading}
        style={{
          padding: '10px 24px',
          background: '#1976d2',
          color: '#fff',
          border: 'none',
          borderRadius: 6,
          cursor: loading ? 'not-allowed' : 'pointer',
          fontSize: 15,
        }}
      >
        {loading ? 'Placing…' : 'Place Bet'}
      </button>
    </div>
  )
}

function friendlyError(code: string): string {
  switch (code) {
    case 'MARKET_NOT_OPEN':   return 'This market is currently closed or suspended.'
    case 'ODDS_CHANGED':      return 'Odds have changed. Please check the new price.'
    case 'LIMIT_EXCEEDED':    return 'This bet exceeds your stake limit.'
    case 'ODDS_NOT_SETTLED':  return 'Our pricing is catching up — please try again in a moment.'
    case 'INSUFFICIENT_FUNDS': return 'Insufficient balance. Please deposit first.'
    default:                  return 'An error occurred. Please try again.'
  }
}
