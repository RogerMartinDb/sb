// API client for the sportsbook backend.
// The Vite dev proxy forwards /api/* → localhost:8080 (Bet Acceptance).
// Bet History queries go to /history-api/* → localhost:8082.

export interface Odds {
  decimal: number
  american: number
}

export interface Selection {
  selection_id: string
  name: string
  target_value: number
  odds_decimal: number
  odds_american: number
}

export interface Market {
  market_id: string
  name: string
  status: string
  market_type: string // "ML" | "SPREAD" | "TOTAL" | "BINARY"
  target_value: number
  is_main: boolean
  selections: Selection[]
}

export interface Event {
  event_id: string
  competition_id: string
  name: string
  starts_at: string
  status: string
  home_score: number
  away_score: number
  game_period: string
  game_clock: string
  markets: Market[]
}

export interface PlaceBetRequest {
  market_id: string
  selection_id: string
  odds_decimal: number
  odds_american: number
  stake_minor: number
  currency: string
}

export interface PlaceBetResponse {
  bet_id: string
  status: string
  odds_decimal: number
  odds_american: number
  stake: number
  currency: string
  placed_at: string
}

export interface Bet {
  bet_id: string
  market_id: string
  selection_id: string
  odds_decimal: number
  odds_american: number
  stake_minor: number
  currency: string
  status: string
  placed_at: string
  settled_at?: string
  payout_minor?: number
}

export class ApiError extends Error {
  constructor(public code: string, public httpStatus: number) {
    super(code)
  }
}

let authToken = ''

export function setAuthToken(token: string | null) {
  authToken = token ?? ''
}

async function post<T>(path: string, body: unknown, idempotencyKey?: string): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (authToken) headers['Authorization'] = `Bearer ${authToken}`
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey

  const res = await fetch(path, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })

  const data = await res.json()
  if (!res.ok) {
    throw new ApiError(data.error ?? 'UNKNOWN', res.status)
  }
  return data as T
}

async function get<T>(path: string): Promise<T> {
  const headers: Record<string, string> = {}
  if (authToken) headers['Authorization'] = `Bearer ${authToken}`

  const res = await fetch(path, { headers })
  const data = await res.json()
  if (!res.ok) throw new ApiError(data.error ?? 'UNKNOWN', res.status)
  return data as T
}

export function getEvents(): Promise<Event[]> {
  return get<Event[]>('/catalog-api/events')
}

export function placeBet(req: PlaceBetRequest, idempotencyKey: string): Promise<PlaceBetResponse> {
  return post<PlaceBetResponse>('/api/bets', req, idempotencyKey)
}

export function getMyBets(): Promise<{ bets: Bet[] }> {
  return get<{ bets: Bet[] }>('/history-api/bets')
}

export async function login(email: string, password: string): Promise<string> {
  const res = await post<{ access_token: string }>('/identity/auth/login', { email, password })
  return res.access_token
}

export async function register(email: string, password: string): Promise<string> {
  const res = await post<{ access_token: string }>('/identity/auth/register', { email, password })
  return res.access_token
}

export interface BalanceResponse {
  available_minor: number;
  currency: string;
}

export interface CashierResponse {
  transaction_id: string;
  status: string;
  available_after: number;
  currency: string;
}

export async function getBalance(token: string): Promise<BalanceResponse> {
  const res = await fetch('/cashier/balance', {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) throw new Error('Failed to fetch balance');
  return res.json();
}

export async function deposit(token: string, amountDollars: number, paymentMethod: string): Promise<CashierResponse> {
  const res = await fetch('/cashier/deposit', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ amount_dollars: amountDollars, payment_method: paymentMethod }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw Object.assign(new Error(data.error || 'Deposit failed'), { status: res.status, data });
  }
  return res.json();
}

export async function withdraw(token: string, amountDollars: number, paymentMethod: string): Promise<CashierResponse> {
  const res = await fetch('/cashier/withdraw', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ amount_dollars: amountDollars, payment_method: paymentMethod }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw Object.assign(new Error(data.error || 'Withdraw failed'), { status: res.status, data });
  }
  return res.json();
}
