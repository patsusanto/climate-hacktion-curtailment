import { actionFill, actionLabel } from './actions'
import { kwh, kw, leadLabel, money, priceMwh, stepTime } from './format'
import type { PlaygroundTick, StepDecision } from './types'

const storyLabel = {
  mid: 'Mid',
  bright: 'Bright roof',
  dull: 'Dull',
  spike: 'Spike',
} as const

const storyStroke = {
  mid: 'var(--ink)',
  bright: 'var(--blue)',
  dull: 'var(--grey)',
  spike: 'var(--yellow)',
} as const

type Props = {
  tick: PlaygroundTick
  decision: StepDecision | null
  loading: boolean
  error: string | null
}

export default function Decision({ tick, decision, loading, error }: Props) {
  const fresh = decision && decision.t === tick.t ? decision : null

  return (
    <div className="decision">
      <div className="readout">
        <span className="chip" style={{ background: actionFill[tick.action] }}>
          {actionLabel[tick.action]}
        </span>
        <span>{stepTime(tick.t)}</span>
        <span>Price {priceMwh(tick.price_aud_mwh)}</span>
        <span>
          Estimate {priceMwh(tick.price_est_p10_aud_mwh)}–{priceMwh(tick.price_est_p90_aud_mwh)}
        </span>
        <span>Roof {kw(tick.pv_kw)}</span>
        <span>Load {kw(tick.load_kw)}</span>
        <span>SoC {kwh(tick.soc_kwh)}</span>
        <span>This step {money(tick.energy_cash_aud)}</span>
      </div>

      {fresh && fresh.leads.length > 0 && (
        <table className="leads">
          <caption className="eyebrow">Forecast from this step</caption>
          <thead>
            <tr>
              <th>Lead</th>
              <th>Price</th>
              <th>Roof</th>
              <th>Load</th>
            </tr>
          </thead>
          <tbody>
            {fresh.leads.map((lead) => (
              <tr key={lead.lead_steps}>
                <td>{leadLabel(lead.lead_steps)}</td>
                <td>
                  {priceMwh(lead.price_p10)}–{priceMwh(lead.price_p90)}
                </td>
                <td>
                  {kw(lead.pv_lo)}–{kw(lead.pv_hi)}
                </td>
                <td>
                  {kw(lead.load_lo)}–{kw(lead.load_hi)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {fresh && fresh.stories.length > 0 && (
        <div className="stories">
          {fresh.stories.map((story) => (
            <article key={story.id} className="story">
              <p className="eyebrow">
                {storyLabel[story.id]} · {story.count}
              </p>
              <Sparkline values={story.price_aud_mwh} color={storyStroke[story.id]} />
              <p className="note">
                {priceMwh(story.price_aud_mwh[0] ?? 0)} to{' '}
                {priceMwh(story.price_aud_mwh[story.price_aud_mwh.length - 1] ?? 0)}
              </p>
            </article>
          ))}
        </div>
      )}

      {loading && <p className="note">Loading the forecast from this step.</p>}
      {error && (
        <p className="note" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}

function Sparkline({ values, color }: { values: number[]; color: string }) {
  if (values.length === 0) return null
  const width = 140
  const height = 36
  const min = Math.min(...values)
  const max = Math.max(...values)
  const span = max - min || 1
  const d = values
    .map((value, index) => {
      const x = values.length === 1 ? width / 2 : (index / (values.length - 1)) * width
      const y = height - 3 - ((value - min) / span) * (height - 8)
      return `${index === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`
    })
    .join('')
  return (
    <svg className="spark" viewBox={`0 0 ${width} ${height}`} aria-hidden="true">
      <path d={d} fill="none" stroke="var(--ink)" strokeOpacity={0.3} strokeWidth={2.6} />
      <path d={d} fill="none" stroke={color} strokeWidth={1.6} strokeLinejoin="round" />
    </svg>
  )
}
