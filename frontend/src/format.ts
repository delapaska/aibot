// Formatting helpers for the panel. The API speaks decimal-string
// rubles; the panel only renders and re-parses them.

const MONTHS_RU = ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']

/** "350.00" -> "350 ₽", "350.50" -> "350.50 ₽" with space grouping. */
export function formatAmount(rubles: string): string {
  const n = Number(rubles)
  if (Number.isNaN(n)) return rubles
  const hasCents = Math.round(n * 100) % 100 !== 0
  const fixed = n.toFixed(hasCents ? 2 : 0)
  const [int, frac] = fixed.split('.')
  const grouped = int.replace(/\B(?=(\d{3})+(?!\d))/g, ' ')
  return frac ? `${grouped}.${frac} ₽` : `${grouped} ₽`
}

/** Kopecks (API create/update payloads talk rubles, panel does too). */
export function formatKopecks(kop: number): string {
  return formatAmount((kop / 100).toFixed(2))
}

/** "350" | "350,50" -> kopecks; null when not a valid positive amount. */
export function parseAmountToKopecks(raw: string): number | null {
  const cleaned = raw.trim().replace(/\s/g, '').replace(',', '.')
  if (!/^\d+(\.\d{1,2})?$/.test(cleaned)) return null
  const kop = Math.round(Number(cleaned) * 100)
  if (!Number.isFinite(kop) || kop <= 0) return null
  return kop
}

/** "2026-10-18" -> "18 окт" (+" 2020" when not the current year). */
export function formatDay(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number)
  if (!y || !m || !d) return iso
  const base = `${d} ${MONTHS_RU[m - 1]}`
  return y === new Date().getFullYear() ? base : `${base} ${y}`
}

/** Today as YYYY-MM-DD in the local time. */
export function todayISO(): string {
  return isoOf(new Date())
}

function isoOf(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

/** Date N days from the given ISO date, same format. */
export function isoOffsetDays(iso: string, days: number): string {
  const [y, m, d] = iso.split('-').map(Number)
  const dt = new Date(y, m - 1, d + days)
  return isoOf(dt)
}
