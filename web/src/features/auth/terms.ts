// CURRENT_TERMS_VERSION mirrors the server constant (01 §Lưu trữ).
export const TERMS_VERSION = 1

// The session cookie is HttpOnly, so the last username lives in localStorage to prefill /login.
export function lastUsername(): string {
  try {
    return localStorage.getItem('unofa.username') ?? ''
  } catch {
    return ''
  }
}

export function rememberUsername(name: string) {
  try {
    localStorage.setItem('unofa.username', name)
  } catch {
    // private mode: nothing to prefill next time
  }
}
