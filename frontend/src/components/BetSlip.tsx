import { useState } from 'react'
import { placeBet, ApiError, type PlaceBetResponse } from '../api'

// Hard-coded demo market. In production these come from the Market Catalog API.
const DEMO_MARKET = {
  market_id: 'mkt-premier-league-arsenal-chelsea-match-result',
  selections: [
    { selection_id: 'sel-arsenal-win',  name: 'Arsenal Win',  odds_num: 5, odds_den: 4 },
    { selection_id: 'sel-draw',         name: 'Draw',          odds_num: 9, odds_den: 5 },
    { selection_id: 'sel-chelsea-win',  name: 'Chelsea Win',   odds_num: 7, odds_den: 4 },
  ],
}

function formatOdds(num: number, den: number) {
  return `${num}/${den}`
}

function uuid() {
  return crypto.randomUUID()
}

export default function BetSlip() {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [stake, setStake] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<PlaceBetResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const selected = DEMO_MARKET.selections.find(s => s.selection_id === selectedId)

  async function handlePlaceBet() {
    if (!selected || !stake) return
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
        market_id: DEMO_MARKET.market_id,
        selection_id: selected.selection_id,
        odds_num: selected.odds_num,
        odds_den: selected.odds_den,
        stake_minor: stakeMinor,
        currency: 'GBP',
      }, uuid())
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
        <p>Odds: {formatOdds(result.odds_num, result.odds_den)}</p>
        <p>Stake: £{(result.stake / 100).toFixed(2)}</p>
        <button onClick={() => { setResult(null); setSelectedId(null); setStake('') }}>
          Place Another Bet
        </button>
      </div>
    )
  }

  return (
    <div>
      <h2 style={{ fontSize: 16, marginBottom: 8 }}>Arsenal vs Chelsea — Match Result</h2>

      <div style={{ display: 'flex', gap: 8, marginBottom: 16, flexWrap: 'wrap' }}>
        {DEMO_MARKET.selections.map(sel => (
          <button
            key={sel.selection_id}
            onClick={() => setSelectedId(sel.selection_id)}
            style={{
              padding: '10px 14px',
              border: '2px solid',
              borderColor: selectedId === sel.selection_id ? '#1976d2' : '#ccc',
              borderRadius: 6,
              background: selectedId === sel.selection_id ? '#e3f2fd' : '#fff',
              cursor: 'pointer',
              textAlign: 'left',
            }}
          >
            <div style={{ fontWeight: 600 }}>{sel.name}</div>
            <div style={{ fontSize: 18, color: '#1976d2' }}>{formatOdds(sel.odds_num, sel.odds_den)}</div>
          </button>
        ))}
      </div>

      {selected && (
        <div style={{ border: '1px solid #ddd', borderRadius: 8, padding: 16 }}>
          <h3 style={{ fontSize: 15, marginBottom: 12 }}>
            Bet Slip — {selected.name} @ {formatOdds(selected.odds_num, selected.odds_den)}
          </h3>

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
              Potential return: £{((parseFloat(stake) * selected.odds_num / selected.odds_den) + parseFloat(stake)).toFixed(2)}
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
      )}
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
