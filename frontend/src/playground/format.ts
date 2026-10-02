const sydney = { timeZone: 'Australia/Sydney', hourCycle: 'h23' } as const

export function money(n: number): string {
  return n.toLocaleString('en-AU', { style: 'currency', currency: 'AUD' })
}

export function num(n: number, digits = 1): string {
  return n.toLocaleString('en-AU', {
    maximumFractionDigits: digits,
    minimumFractionDigits: digits,
  })
}

export function priceMwh(n: number): string {
  const digits = Math.abs(n) >= 100 ? 0 : 1
  return n.toLocaleString('en-AU', {
    maximumFractionDigits: digits,
    minimumFractionDigits: digits,
  })
}

export function kwh(n: number): string {
  return `${num(n, Math.abs(n) >= 10 ? 1 : 2)} kWh`
}

export function kw(n: number): string {
  return `${num(n, 2)} kW`
}

export function stepTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return new Intl.DateTimeFormat('en-AU', {
    ...sydney,
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function dayLabel(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return new Intl.DateTimeFormat('en-AU', {
    ...sydney,
    day: 'numeric',
    month: 'short',
  }).format(date)
}

/** end is exclusive, so the label stops on the last step. */
export function windowRange(start: string, end: string): string {
  const last = new Date(new Date(end).getTime() - 5 * 60 * 1000)
  if (Number.isNaN(last.getTime())) return `${start} – ${end}`
  return `${dayLabel(start)} – ${dayLabel(last.toISOString())}`
}

export function leadLabel(steps: number): string {
  const minutes = steps * 5
  if (minutes < 60) return `${minutes} min`
  const hours = minutes / 60
  return Number.isInteger(hours) ? `${hours} h` : `${hours.toFixed(1)} h`
}

export function axisNum(v: number): string {
  const abs = Math.abs(v)
  if (abs >= 100 || Math.abs(v - Math.round(v)) < 1e-6) return Math.round(v).toString()
  if (abs >= 10) return v.toFixed(0)
  return v.toFixed(1)
}
