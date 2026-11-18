// Integer loan math from 03 §Công thức, for preview only: the server's number wins.
const roundHalfUp = (n: number) => Math.floor((n + 5000) / 10000)

export function owedInTerm(principal: number, rateBps: number): number {
  return roundHalfUp(principal * (10000 + rateBps))
}

export function owedAfter(principal: number, rateBps: number, periods: number, overdueBps = 500, capMultiplier = 2): number {
  const cap = principal * capMultiplier
  let owed = owedInTerm(principal, rateBps)
  for (let i = 0; i < periods && owed < cap; i++) owed = roundHalfUp(owed * (10000 + overdueBps))
  return Math.min(owed, cap)
}

// periodsToSeize is the first overdue period whose owed reaches the cap.
export function periodsToSeize(principal: number, rateBps: number, overdueBps = 500, capMultiplier = 2): number {
  let n = 0
  while (owedAfter(principal, rateBps, n, overdueBps, capMultiplier) < principal * capMultiplier) n++
  return n
}

// forgivenIfSeized mirrors 03 §Seize: seized = min(owed, balance), forgiven = owed - seized.
export function forgivenIfSeized(owed: number, balance: number): number {
  return owed - Math.min(owed, Math.max(0, balance))
}
