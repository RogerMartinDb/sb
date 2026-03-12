import { useState } from 'react'
import { placeBet, ApiError, type PlaceBetResponse } from '../api'
import type { SelectedBet } from './EventList'
import type { OddsFormat } from '../App'

interface Props {
  selectedBet: SelectedBet | null
  onClear: () => void
  onBetPlaced?: () => void
  oddsFormat?: OddsFormat
  token?: string | null
  onLoginRequired?: () => void
}

function formatOdds(decimal: number, american: number, format: OddsFormat = 'american'): string {
  switch (format) {
    case 'decimal': return decimal.toFixed(2)
    case 'cent':    return (100 / decimal).toFixed(1)
    default:        return american >= 0 ? `+${american}` : `${american}`
  }
}

export default function BetSlip({ selectedBet, onClear, onBetPlaced, oddsFormat = 'american', token, onLoginRequired }: Props) {
  const [stake, setStake] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<PlaceBetResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const containerStyle = {
    background: '#0d1f3c',
    color: '#fff',
    borderRadius: 8,
    padding: 16,
  }

  if (!selectedBet) {
    return (
      <div style={{ ...containerStyle, border: '1px solid #1c3354' }}>
        <h3 style={{ fontSize: 15, margin: '0 0 12px', color: '#fff' }}>Bet Slip</h3>
        <p style={{ fontSize: 13, color: '#6b849e', margin: 0 }}>Select a market to add a bet.</p>
      </div>
    )
  }

  async function handlePlaceBet() {
    if (!token) {
      onLoginRequired?.()
      return
    }

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
        market_name: selectedBet.market_name,
        selection_id: selectedBet.selection_id,
        selection_name: selectedBet.selection_name,
        odds_decimal: selectedBet.odds_decimal,
        odds_american: selectedBet.odds_american,
        stake_minor: stakeMinor,
        currency: 'USD',
      }, crypto.randomUUID())
      setResult(resp)
      onBetPlaced?.()
      setTimeout(() => { onClear() }, 1500)
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
      <div style={{ ...containerStyle, border: '1px solid #4caf50' }}>
        <h3 style={{ color: '#4caf50', margin: '0 0 12px' }}>Bet Accepted</h3>
        <p style={{ margin: '0 0 6px' }}>Bet ID: <code>{result.bet_id}</code></p>
        <p style={{ margin: '0 0 6px' }}>Odds: {formatOdds(result.odds_decimal, result.odds_american, oddsFormat)}</p>
        <p style={{ margin: '0 0 12px' }}>Stake: ${(result.stake / 100).toFixed(2)}</p>
        <button onClick={() => { setResult(null); setStake(''); onClear() }}>
          Place Another Bet
        </button>
      </div>
    )
  }

  return (
    <div style={{ ...containerStyle, border: '1px solid #1c3354' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <h3 style={{ fontSize: 15, margin: 0, color: '#fff' }}>Bet Slip</h3>
        <button onClick={onClear} style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#6b849e', fontSize: 16 }}>✕</button>
      </div>

      <p style={{ fontSize: 14, marginBottom: 4, color: '#a0b4c8' }}>{selectedBet.market_name}</p>
      <p style={{ fontSize: 14, fontWeight: 600, marginBottom: 12, color: '#fff' }}>
        {selectedBet.selection_name} @ {formatOdds(selectedBet.odds_decimal, selectedBet.odds_american, oddsFormat)}
      </p>

      <div style={{ marginBottom: 12 }}>
        <label style={{ color: '#a0b4c8', fontSize: 13 }}>
          Stake ($)
          <br />
          <input
            type="number"
            min="0.01"
            step="0.01"
            value={stake}
            onChange={e => setStake(e.target.value)}
            style={{ padding: 8, width: 120, marginTop: 4, background: '#07152b', color: '#fff', border: '1px solid #1c3354', borderRadius: 4 }}
            placeholder="0.00"
          />
        </label>
      </div>

      {stake && !isNaN(parseFloat(stake)) && parseFloat(stake) > 0 && (
        <p style={{ fontSize: 13, color: '#a0b4c8', marginBottom: 12 }}>
          Potential return: ${(parseFloat(stake) * selectedBet.odds_decimal).toFixed(2)}
        </p>
      )}

      {error && <p style={{ color: '#e74c3c', fontSize: 14 }}>{error}</p>}

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
