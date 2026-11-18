export class ApiError extends Error {
  constructor(
    public code: string,
    public status: number,
    public requestId?: string,
    public retryAfter?: number,
  ) {
    super(code)
  }
}

type Init = Omit<RequestInit, 'body'> & { json?: unknown }

// Each app (game, admin) decides how a failure surfaces: toasts, relogin, logout.
export interface ApiHooks {
  network(): void
  failed(err: ApiError): void
}
let hooks: ApiHooks = { network() {}, failed() {} }
export const setApiHooks = (h: ApiHooks) => {
  hooks = h
}

// api is the only fetch in the app (06 §API client). It never retries: POSTs that
// are safe to resend carry their own client_uuid.
export async function api<T>(path: string, init: Init = {}): Promise<T> {
  const { json, ...rest } = init
  const headers = new Headers(rest.headers)
  headers.set('Accept', 'application/json')
  if (json !== undefined) headers.set('Content-Type', 'application/json')

  let res: Response
  try {
    res = await fetch(path, {
      ...rest,
      headers,
      credentials: 'include',
      body: json === undefined ? undefined : JSON.stringify(json),
    })
  } catch {
    hooks.network()
    throw new ApiError('NETWORK', 0)
  }

  if (res.status === 204) return undefined as T
  if (res.ok) return (await res.json()) as T

  let body: { error?: string; request_id?: string } = {}
  try {
    body = await res.json()
  } catch {
    // nginx-level 429/502 pages are not JSON.
  }
  const retry = Number(res.headers.get('Retry-After')) || undefined
  const code = body.error ?? (res.status === 429 ? 'RATE_LIMITED' : res.status >= 500 ? 'SERVICE_UNAVAILABLE' : 'INTERNAL')
  const err = new ApiError(code, res.status, body.request_id, retry)

  hooks.failed(err)
  throw err
}

export const isApiError = (e: unknown, code?: string): e is ApiError =>
  e instanceof ApiError && (code === undefined || e.code === code)
