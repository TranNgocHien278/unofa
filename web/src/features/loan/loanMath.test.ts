import { describe, expect, it } from 'vitest'
import { forgivenIfSeized, owedAfter, owedInTerm, periodsToSeize } from './loanMath'

describe('loan math golden case (03 §Ví dụ số học)', () => {
  it('500U/24h: 505, 530, 557', () => {
    expect(owedInTerm(500, 100)).toBe(505)
    expect(owedAfter(500, 100, 1)).toBe(530)
    expect(owedAfter(500, 100, 2)).toBe(557)
  })
  it('hits the 2x cap at n=14, not 15', () => {
    expect(owedAfter(500, 100, 13)).toBeLessThan(1000)
    expect(owedAfter(500, 100, 14)).toBe(1000)
    expect(periodsToSeize(500, 100)).toBe(14)
  })
  it('caps 2000U at 4000U', () => {
    expect(owedAfter(2000, 1000, 60)).toBe(4000)
  })
  it('forgives what the balance cannot cover', () => {
    expect(forgivenIfSeized(1000, 300)).toBe(700)
    expect(forgivenIfSeized(1000, 5000)).toBe(0)
    expect(forgivenIfSeized(1000, -5)).toBe(1000)
  })
})
