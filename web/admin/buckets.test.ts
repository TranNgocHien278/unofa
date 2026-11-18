import { describe, expect, it } from 'vitest'
import { codeClassRows, OTHER_KEY, sumKeys, topSeries } from './buckets'

const at = '2026-11-18T10:00:00Z'

describe('topSeries', () => {
  it('keeps the busiest endpoints and folds the rest into Other', () => {
    const { keys, rows } = topSeries([{ at, by_endpoint: { '/a': 5, '/b': 9, '/c': 1, '/d': 2 } }], 2)
    expect(keys).toEqual(['/b', '/a', OTHER_KEY])
    expect(rows[0]).toMatchObject({ '/b': 9, '/a': 5, [OTHER_KEY]: 3 })
  })
  it('adds no Other slot when everything fits', () => {
    expect(topSeries([{ at, by_endpoint: { '/a': 1 } }], 7).keys).toEqual(['/a'])
  })
})

describe('codeClassRows', () => {
  it('sums codes into classes', () => {
    const rows = codeClassRows([{ at, by_code: { '200': 10, '204': 2, '429': 3, '503': 1 } }])
    expect(rows[0]).toMatchObject({ '2xx': 12, '3xx': 0, '4xx': 3, '5xx': 1 })
    expect(sumKeys(rows, ['4xx', '5xx'])).toBe(4)
  })
})
