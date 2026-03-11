import { useState } from 'react';
import { deposit, withdraw } from '../api';

interface Props {
  mode: 'deposit' | 'withdraw';
  token: string;
  onClose: () => void;
  onSuccess: (newBalanceMinor: number) => void;
}

const PAYMENT_METHODS = [
  { value: 'CARD', label: 'Card' },
  { value: 'BITCOIN', label: 'Bitcoin' },
  { value: 'USDC', label: 'USD Coin' },
  { value: 'ETHEREUM', label: 'Ethereum' },
];

export default function CashierModal({ mode, token, onClose, onSuccess }: Props) {
  const [amount, setAmount] = useState('');
  const [paymentMethod, setPaymentMethod] = useState('CARD');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const amountDollars = parseFloat(amount);
    if (isNaN(amountDollars) || amountDollars <= 0) {
      setError('Please enter a valid amount');
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const fn = mode === 'deposit' ? deposit : withdraw;
      const res = await fn(token, amountDollars, paymentMethod);
      onSuccess(res.available_after);
    } catch (err: any) {
      if (err.data?.error === 'INSUFFICIENT_FUNDS') {
        setError('Insufficient funds');
      } else {
        setError(err.message || 'Something went wrong');
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.7)',
      display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
    }}>
      <div style={{
        background: '#1a1a2e', border: '1px solid #333', borderRadius: 8,
        padding: 24, minWidth: 320, color: '#fff',
      }}>
        <h2 style={{ marginTop: 0, textTransform: 'capitalize' }}>{mode}</h2>
        <form onSubmit={handleSubmit}>
          <div style={{ marginBottom: 16 }}>
            <label style={{ display: 'block', marginBottom: 4, color: '#aaa', fontSize: 12 }}>Amount</label>
            <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <span style={{ color: '#aaa' }}>$</span>
              <input
                type="number"
                min="0.01"
                step="0.01"
                value={amount}
                onChange={e => setAmount(e.target.value)}
                placeholder="0.00"
                style={{
                  flex: 1, background: '#0f0f1a', border: '1px solid #444',
                  borderRadius: 4, padding: '8px 12px', color: '#fff', fontSize: 16,
                }}
              />
            </div>
          </div>
          <div style={{ marginBottom: 16 }}>
            <label style={{ display: 'block', marginBottom: 4, color: '#aaa', fontSize: 12 }}>Payment Method</label>
            <select
              value={paymentMethod}
              onChange={e => setPaymentMethod(e.target.value)}
              style={{
                width: '100%', background: '#0f0f1a', border: '1px solid #444',
                borderRadius: 4, padding: '8px 12px', color: '#fff', fontSize: 14,
              }}
            >
              {PAYMENT_METHODS.map(m => (
                <option key={m.value} value={m.value}>{m.label}</option>
              ))}
            </select>
          </div>
          {error && <p style={{ color: '#f44', marginBottom: 12 }}>{error}</p>}
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              type="submit"
              disabled={loading}
              style={{
                flex: 1, background: '#4CAF50', border: 'none', borderRadius: 4,
                padding: '10px 0', color: '#fff', fontWeight: 'bold', cursor: loading ? 'not-allowed' : 'pointer',
              }}
            >
              {loading ? '...' : mode === 'deposit' ? 'Deposit' : 'Withdraw'}
            </button>
            <button
              type="button"
              onClick={onClose}
              style={{
                flex: 1, background: '#333', border: 'none', borderRadius: 4,
                padding: '10px 0', color: '#fff', cursor: 'pointer',
              }}
            >
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
