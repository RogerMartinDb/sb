// API client for the sportsbook backend.
// The Vite dev proxy forwards /api/* → localhost:8080 (Bet Acceptance).
// Bet History queries go to /history-api/* → localhost:8082.

export interface Odds {
  num: number
  den: number
}

export interface PlaceBetRequest {
  market_id: string
  selection_id: string
  odds_num: number
  odds_den: number
  stake_minor: number
  currency: string
}

export interface PlaceBetResponse {
  bet_id: string
  status: string
  odds_num: number
  odds_den: number
  stake: number
  currency: string
  placed_at: string
}

export interface Bet {
  bet_id: string
  market_id: string
  selection_id: string
  odds_num: number
  odds_den: number
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

export function setAuthToken(token: string) {
  authToken = token
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
