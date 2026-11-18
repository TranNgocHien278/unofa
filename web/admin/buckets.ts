// Shapes server buckets for charts. The server already buckets by time (06 §Chart);
// this only folds series: endpoints beyond the top N into "Other", codes into classes.

type Bucket<K extends string> = { at: string } & Record<K, Record<string, number>>
export type Row = { t: number } & Record<string, number>

export const CLASSES = ['2xx', '3xx', '4xx', '5xx'] as const
export const OTHER_KEY = '__other'

// topSeries keeps the N busiest endpoints (fixed slots, never cycled) and folds the rest.
export function topSeries(buckets: Bucket<'by_endpoint'>[], n = 7): { keys: string[]; rows: Row[] } {
  const totals = new Map<string, number>()
  for (const b of buckets) for (const [k, v] of Object.entries(b.by_endpoint)) totals.set(k, (totals.get(k) ?? 0) + v)
  const ranked = [...totals.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).map(([k]) => k)
  const keep = ranked.slice(0, n)
  const fold = ranked.length > n
  const rows = buckets.map((b) => {
    const row: Row = { t: Date.parse(b.at) }
    for (const k of keep) row[k] = b.by_endpoint[k] ?? 0
    if (fold) row[OTHER_KEY] = Object.entries(b.by_endpoint).reduce((s, [k, v]) => (keep.includes(k) ? s : s + v), 0)
    return row
  })
  return { keys: fold ? [...keep, OTHER_KEY] : keep, rows }
}

export function codeClassRows(buckets: Bucket<'by_code'>[]): Row[] {
  return buckets.map((b) => {
    const row: Row = { t: Date.parse(b.at), '2xx': 0, '3xx': 0, '4xx': 0, '5xx': 0 }
    for (const [code, v] of Object.entries(b.by_code)) {
      const cls = `${code[0]}xx`
      if (cls in row) row[cls] += v
    }
    return row
  })
}

export const sumKeys = (rows: Row[], keys: readonly string[]) => rows.reduce((s, r) => s + keys.reduce((a, k) => a + (r[k] ?? 0), 0), 0)
