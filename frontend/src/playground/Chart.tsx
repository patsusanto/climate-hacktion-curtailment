import { useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { MouseEvent as ReactMouseEvent } from 'react'
import { actionFill, actionLabel, actionOrder } from './actions'
import { axisNum, dayLabel } from './format'
import type { Action, PlaygroundTick } from './types'

type Props = {
  ticks: PlaygroundTick[]
  total: number
  start: string
  stepMinutes: number
  active: number | null
  waiting: boolean
  onHover: (i: number | null) => void
  onPick: (i: number) => void
}

export default function Chart({
  ticks,
  total,
  start,
  stepMinutes,
  active,
  waiting,
  onHover,
  onPick,
}: Props) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const clip = useId().replace(/:/g, '')

  useLayoutEffect(() => {
    const el = wrapRef.current
    if (!el) return
    const apply = () => setWidth(el.clientWidth)
    apply()
    const observer = new ResizeObserver(apply)
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const n = Math.max(total, ticks.length, 1)
  const padL = 60
  const padR = 16
  const priceTop = 22
  const priceH = 200
  const actionTop = priceTop + priceH + 10
  const actionH = 16
  const saveTop = actionTop + actionH + 22
  const saveH = 104
  const height = saveTop + saveH + 32
  const plotW = Math.max(1, width - padL - padR)

  const xMid = (i: number) => padL + ((i + 0.5) / n) * plotW
  const xEdge = (i: number) => padL + (i / n) * plotW

  const priceLine = useMemo(() => priceSamples(ticks, 800), [ticks])
  const saveLine = useMemo(() => savingsSamples(ticks, 800), [ticks])
  const lanes = useMemo(() => segments(ticks), [ticks])

  const priceDomain = useMemo(() => {
    if (ticks.length === 0) return [0, 300] as [number, number]
    const values: number[] = []
    for (const tick of ticks) {
      values.push(tick.price_aud_mwh, tick.price_est_p10_aud_mwh, tick.price_est_p90_aud_mwh)
    }
    return extent(values)
  }, [ticks])

  const saveDomain = useMemo(() => {
    if (ticks.length === 0) return [-1, 1] as [number, number]
    const values: number[] = []
    for (const tick of ticks) values.push(-tick.cumulative_self_aud, tick.cumulative_savings_aud)
    return extent(values, true)
  }, [ticks])

  const yPrice = (v: number) => scale(v, priceDomain[0], priceDomain[1], priceTop, priceH)
  const ySave = (v: number) => scale(v, saveDomain[0], saveDomain[1], saveTop, saveH)

  const band = priceLine.map((tick) => ({
    x: xMid(tick.i),
    y0: yPrice(tick.price_est_p10_aud_mwh),
    y1: yPrice(tick.price_est_p90_aud_mwh),
  }))
  const actual = priceLine.map((tick) => ({ x: xMid(tick.i), y: yPrice(tick.price_aud_mwh) }))
  const mid = priceLine.map((tick) => ({ x: xMid(tick.i), y: yPrice(tick.price_est_p50_aud_mwh) }))
  const lost = saveLine.map((tick) => ({ x: xMid(tick.i), y: ySave(-tick.cumulative_self_aud) }))
  const saved = saveLine.map((tick) => ({ x: xMid(tick.i), y: ySave(tick.cumulative_savings_aud) }))

  const focus = active != null && ticks[active]?.i === active ? ticks[active] : null
  const priceTicks = axisTicks(priceDomain[0], priceDomain[1], 4)
  const saveTicks = axisTicks(saveDomain[0], saveDomain[1], 4)
  const xLabels = axisIndexes(n, width < 560 ? 2 : 4)
  const midnights = midnightIndexes(n, stepMinutes)

  function indexAt(event: ReactMouseEvent<SVGRectElement>): number | null {
    const svg = event.currentTarget.ownerSVGElement
    if (!svg || ticks.length === 0) return null
    const point = svg.createSVGPoint()
    point.x = event.clientX
    point.y = event.clientY
    const matrix = svg.getScreenCTM()
    if (!matrix) return null
    const local = point.matrixTransform(matrix.inverse())
    if (local.x < padL || local.x > padL + plotW) return null
    const i = Math.floor(((local.x - padL) / plotW) * n)
    if (ticks[i]?.i !== i) return null
    return i
  }

  return (
    <div>
      <div ref={wrapRef} className="chart-wrap">
        {width > 0 && (
          <svg
            className="chart-svg"
            width={width}
            height={height}
            aria-hidden="true"
            style={{ fontFamily: '"DM Sans", Georgia, sans-serif' }}
          >
            <defs>
              <clipPath id={`${clip}-price`}>
                <rect x={padL} y={priceTop} width={plotW} height={priceH} />
              </clipPath>
              <clipPath id={`${clip}-save`}>
                <rect x={padL} y={saveTop} width={plotW} height={saveH} />
              </clipPath>
            </defs>

            {priceTicks.map((value) => (
              <g key={`p-${value}`}>
                <line
                  x1={padL}
                  x2={padL + plotW}
                  y1={yPrice(value)}
                  y2={yPrice(value)}
                  stroke="var(--ink)"
                  strokeOpacity={0.12}
                />
                <text
                  x={padL - 8}
                  y={yPrice(value)}
                  textAnchor="end"
                  dominantBaseline="middle"
                  fill="var(--grey)"
                  fontSize={11}
                >
                  {axisNum(value)}
                </text>
              </g>
            ))}

            {saveTicks.map((value) => (
              <g key={`s-${value}`}>
                <line
                  x1={padL}
                  x2={padL + plotW}
                  y1={ySave(value)}
                  y2={ySave(value)}
                  stroke="var(--ink)"
                  strokeOpacity={0.12}
                />
                <text
                  x={padL - 8}
                  y={ySave(value)}
                  textAnchor="end"
                  dominantBaseline="middle"
                  fill="var(--grey)"
                  fontSize={11}
                >
                  {axisNum(value)}
                </text>
              </g>
            ))}

            {midnights.map((i) => (
              <line
                key={`d-${i}`}
                x1={xEdge(i)}
                x2={xEdge(i)}
                y1={priceTop}
                y2={saveTop + saveH}
                stroke="var(--ink)"
                strokeOpacity={0.12}
              />
            ))}

            <g clipPath={`url(#${clip}-price)`}>
              {band.length > 1 && <path d={areaPath(band)} fill="var(--grey)" fillOpacity={0.22} />}
              {mid.length > 1 && (
                <path
                  d={linePath(mid)}
                  fill="none"
                  stroke="var(--grey)"
                  strokeWidth={1.25}
                  strokeDasharray="4 4"
                  strokeLinejoin="round"
                />
              )}
              {actual.length > 1 && (
                <path
                  d={linePath(actual)}
                  fill="none"
                  stroke="var(--ink)"
                  strokeWidth={1.8}
                  strokeLinejoin="round"
                  strokeLinecap="round"
                />
              )}
              {actual.length === 1 && <circle cx={actual[0].x} cy={actual[0].y} r={3} fill="var(--ink)" />}
            </g>

            {lanes.map((lane) => (
              <rect
                key={`${lane.action}-${lane.i0}`}
                x={xEdge(lane.i0)}
                y={actionTop}
                width={Math.max(1, xEdge(lane.i1 + 1) - xEdge(lane.i0))}
                height={actionH}
                fill={actionFill[lane.action]}
              />
            ))}

            <g clipPath={`url(#${clip}-save)`}>
              {lost.length > 1 && (
                <path
                  d={linePath(lost)}
                  fill="none"
                  stroke="var(--loss)"
                  strokeWidth={1.75}
                  strokeLinejoin="round"
                  strokeLinecap="round"
                />
              )}
              {saved.length > 1 && (
                <path
                  d={linePath(saved)}
                  fill="none"
                  stroke="var(--gain)"
                  strokeWidth={2.25}
                  strokeLinejoin="round"
                  strokeLinecap="round"
                />
              )}
              {lost.length === 1 && <circle cx={lost[0].x} cy={lost[0].y} r={3} fill="var(--loss)" />}
              {saved.length === 1 && <circle cx={saved[0].x} cy={saved[0].y} r={3} fill="var(--gain)" />}
            </g>

            {focus && (
              <g>
                <line
                  x1={xMid(focus.i)}
                  x2={xMid(focus.i)}
                  y1={priceTop}
                  y2={saveTop + saveH}
                  stroke="var(--ink)"
                />
                <circle cx={xMid(focus.i)} cy={yPrice(focus.price_aud_mwh)} r={3.5} fill="var(--ink)" />
                <circle
                  cx={xMid(focus.i)}
                  cy={ySave(-focus.cumulative_self_aud)}
                  r={3.5}
                  fill="var(--loss)"
                />
                <circle
                  cx={xMid(focus.i)}
                  cy={ySave(focus.cumulative_savings_aud)}
                  r={3.5}
                  fill="var(--gain)"
                  stroke="var(--ink)"
                />
              </g>
            )}

            {xLabels.map((i, index) => {
              const instant = new Date(Date.parse(start) + i * stepMinutes * 60 * 1000)
              const spanHours = ((n - 1) * stepMinutes) / 60
              const label =
                spanHours > 36
                  ? dayLabel(instant.toISOString())
                  : new Intl.DateTimeFormat('en-AU', {
                      timeZone: 'Australia/Sydney',
                      hour: '2-digit',
                      minute: '2-digit',
                      hourCycle: 'h23',
                    }).format(instant)
              const anchor = index === 0 ? 'start' : index === xLabels.length - 1 ? 'end' : 'middle'
              const x = anchor === 'start' ? padL : anchor === 'end' ? padL + plotW : xMid(i)
              return (
                <text
                  key={`x-${i}`}
                  x={x}
                  y={height - 12}
                  textAnchor={anchor}
                  fill="var(--grey)"
                  fontSize={11}
                >
                  {label}
                </text>
              )
            })}

            {waiting && ticks.length === 0 && (
              <text
                x={padL + plotW / 2}
                y={priceTop + priceH / 2}
                textAnchor="middle"
                fill="var(--grey)"
                fontSize={15}
              >
                Waiting for the first step
              </text>
            )}

            <rect
              className="plot-hit"
              x={padL}
              y={priceTop}
              width={plotW}
              height={saveTop + saveH - priceTop}
              fill="transparent"
              onPointerMove={(event) => onHover(indexAt(event))}
              onPointerLeave={() => onHover(null)}
              onClick={(event) => {
                const i = indexAt(event)
                if (i != null) onPick(i)
              }}
            />
          </svg>
        )}
      </div>
      <ul className="legend">
        <li>
          <i className="swatch swatch-price" /> Price
        </li>
        <li>
          <i className="swatch swatch-band" /> Estimate
        </li>
        <li>
          <i className="swatch swatch-loss" /> Self-consumption
        </li>
        <li>
          <i className="swatch swatch-save" /> Saved
        </li>
        {actionOrder.map((action) => (
          <li key={action}>
            <i className="swatch" style={{ background: actionFill[action] }} />
            {actionLabel[action]}
          </li>
        ))}
      </ul>
    </div>
  )
}

function scale(value: number, min: number, max: number, top: number, height: number): number {
  const span = max - min || 1
  return top + height - ((value - min) / span) * height
}

function extent(values: number[], includeZero = false): [number, number] {
  let min = includeZero ? 0 : Infinity
  let max = includeZero ? 0 : -Infinity
  for (const value of values) {
    if (!Number.isFinite(value)) continue
    if (value < min) min = value
    if (value > max) max = value
  }
  if (!Number.isFinite(min) || min === Infinity) return [0, 1]
  if (min === max) {
    min -= 1
    max += 1
  }
  const pad = (max - min) * 0.08
  return [min - pad, max + pad]
}

function niceStep(rough: number): number {
  if (!Number.isFinite(rough) || rough <= 0) return 1
  const pow = 10 ** Math.floor(Math.log10(rough))
  const fraction = rough / pow
  const nice = fraction >= 7.5 ? 10 : fraction >= 3.5 ? 5 : fraction >= 1.5 ? 2 : 1
  return nice * pow
}

function axisTicks(min: number, max: number, count: number): number[] {
  const step = niceStep((max - min) / Math.max(1, count - 1))
  const start = Math.ceil((min - step * 1e-6) / step) * step
  const out: number[] = []
  for (let value = start; value <= max + step * 1e-6; value += step) {
    out.push(Number(value.toFixed(6)))
    if (out.length > 6) break
  }
  return out.length > 0 ? out : [min, max]
}

function axisIndexes(total: number, count: number): number[] {
  if (total <= 1) return [0]
  const last = total - 1
  const out: number[] = []
  for (let k = 0; k < count; k++) {
    out.push(Math.round((last * k) / Math.max(1, count - 1)))
  }
  return [...new Set(out)]
}

function midnightIndexes(total: number, stepMinutes: number): number[] {
  const perDay = Math.round((24 * 60) / stepMinutes)
  const out: number[] = []
  for (let i = perDay; i < total; i += perDay) out.push(i)
  return out
}

function priceSamples(ticks: PlaygroundTick[], max: number): PlaygroundTick[] {
  if (ticks.length <= max) return ticks
  const out: PlaygroundTick[] = []
  const size = ticks.length / max
  for (let bucket = 0; bucket < max; bucket++) {
    const start = Math.floor(bucket * size)
    const end = Math.min(ticks.length, Math.max(start + 1, Math.floor((bucket + 1) * size)))
    let lo = ticks[start]
    let hi = ticks[start]
    for (let j = start; j < end; j++) {
      if (ticks[j].price_aud_mwh < lo.price_aud_mwh) lo = ticks[j]
      if (ticks[j].price_aud_mwh > hi.price_aud_mwh) hi = ticks[j]
    }
    if (lo.i <= hi.i) out.push(lo, hi)
    else out.push(hi, lo)
  }
  return out
}

function savingsSamples(ticks: PlaygroundTick[], max: number): PlaygroundTick[] {
  if (ticks.length <= max) return ticks
  const out: PlaygroundTick[] = []
  const size = ticks.length / max
  for (let bucket = 0; bucket < max; bucket++) {
    const end = Math.min(ticks.length, Math.floor((bucket + 1) * size)) - 1
    out.push(ticks[Math.max(0, end)])
  }
  const last = ticks[ticks.length - 1]
  if (out[out.length - 1]?.i !== last.i) out.push(last)
  return out
}

function segments(ticks: PlaygroundTick[]): Array<{ action: Action; i0: number; i1: number }> {
  if (ticks.length === 0) return []
  const out: Array<{ action: Action; i0: number; i1: number }> = []
  let action = ticks[0].action
  let i0 = ticks[0].i
  let prev = ticks[0].i
  for (let k = 1; k < ticks.length; k++) {
    const tick = ticks[k]
    if (tick.action !== action) {
      out.push({ action, i0, i1: prev })
      action = tick.action
      i0 = tick.i
    }
    prev = tick.i
  }
  out.push({ action, i0, i1: prev })
  return out
}

function linePath(points: Array<{ x: number; y: number }>): string {
  return points
    .map((point, index) => `${index === 0 ? 'M' : 'L'}${round(point.x)} ${round(point.y)}`)
    .join('')
}

function areaPath(points: Array<{ x: number; y0: number; y1: number }>): string {
  if (points.length === 0) return ''
  const forward = points
    .map((point, index) => `${index === 0 ? 'M' : 'L'}${round(point.x)} ${round(point.y1)}`)
    .join('')
  const back = [...points]
    .reverse()
    .map((point) => `L${round(point.x)} ${round(point.y0)}`)
    .join('')
  return `${forward}${back}Z`
}

function round(value: number): number {
  return Math.round(value * 10) / 10
}
