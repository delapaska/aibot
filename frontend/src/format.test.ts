import { describe, expect, it } from 'vitest'
import { formatAmount, formatKopecks, parseAmountToKopecks, formatDay, todayISO, isoOffsetDays } from './format'

describe('formatAmount', () => {
  it('shows a decimal-string amount without trailing zeros', () => {
    expect(formatAmount('350.00')).toBe('350 ₽')
    expect(formatAmount('350.50')).toBe('350.50 ₽')
  })

  it('uses thin grouping for thousands', () => {
    expect(formatAmount('12345.00')).toBe('12 345 ₽')
  })

  it('keeps the sign for negatives (defensive)', () => {
    expect(formatAmount('-10.00')).toBe('-10 ₽')
  })
})

describe('formatKopecks', () => {
  it('renders kopecks as rubles', () => {
    expect(formatKopecks(35000)).toBe('350 ₽')
    expect(formatKopecks(1234567)).toBe('12 345.67 ₽')
  })
})

describe('parseAmountToKopecks', () => {
  it('parses a normal ruble amount', () => {
    expect(parseAmountToKopecks('350')).toBe(35000)
    expect(parseAmountToKopecks('350.50')).toBe(35050)
  })

  it('accepts comma as the decimal separator', () => {
    expect(parseAmountToKopecks('350,50')).toBe(35050)
  })

  it('trims spaces and thousands separators', () => {
    expect(parseAmountToKopecks(' 1 234,56 ')).toBe(123456)
  })

  it('rejects empty and non-numeric input', () => {
    expect(parseAmountToKopecks('')).toBeNull()
    expect(parseAmountToKopecks('abc')).toBeNull()
    expect(parseAmountToKopecks('12.3.4')).toBeNull()
  })

  it('rejects more than two decimal digits', () => {
    expect(parseAmountToKopecks('1.005')).toBeNull()
  })

  it('rejects non-positive amounts', () => {
    expect(parseAmountToKopecks('0')).toBeNull()
    expect(parseAmountToKopecks('-5')).toBeNull()
  })
})

describe('formatDay', () => {
  it('renders YYYY-MM-DD in Russian day-month', () => {
    expect(formatDay('2026-10-18')).toBe('18 окт')
    expect(formatDay('2026-01-09')).toBe('9 янв')
  })

  it('adds the year when it differs from the current one', () => {
    const y = new Date().getFullYear()
    expect(formatDay(`2020-05-01`)).toBe(`1 мая 2020`)
    void y
  })
})

describe('todayISO', () => {
  it('returns today in YYYY-MM-DD', () => {
    const now = new Date()
    const expected = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    expect(todayISO()).toBe(expected)
  })
})

describe('isoOffsetDays', () => {
  it('shifts the date by N days', () => {
    expect(isoOffsetDays('2026-10-18', -7)).toBe('2026-10-11')
    expect(isoOffsetDays('2026-10-01', -1)).toBe('2026-09-30')
    expect(isoOffsetDays('2026-10-18', 0)).toBe('2026-10-18')
  })
})
