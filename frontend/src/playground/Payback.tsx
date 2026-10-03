import { useState } from 'react'
import { money } from './format'
import type { Payback as PaybackFigures, PlaygroundMeta } from './types'

type Props = {
  payback: PaybackFigures
  spec: PlaygroundMeta['spec']
}

/** A year of bills three ways, what the system costs, and how fast each way repays it. */
export default function Payback({ payback, spec }: Props) {
  const [solarPerKw, setSolarPerKw] = useState(String(payback.solar_aud_per_kw))
  const [batteryPerKwh, setBatteryPerKwh] = useState(String(payback.battery_aud_per_kwh))

  const solarRate = positive(solarPerKw, payback.solar_aud_per_kw)
  const batteryRate = positive(batteryPerKwh, payback.battery_aud_per_kwh)
  const cost =
    spec.pv_kw_ac * solarRate + Math.max(spec.battery_kwh * batteryRate - payback.battery_rebate_aud, 0)

  const none = payback.annual_bill_no_system_aud
  const selfSaves = none - payback.annual_bill_self_consumption_aud
  const plannerSaves = none - payback.annual_bill_planner_aud
  const selfYears = years(cost, selfSaves)
  const plannerYears = years(cost, plannerSaves)
  const sooner = selfYears != null && plannerYears != null ? selfYears - plannerYears : null

  return (
    <section className="card payback" aria-labelledby="payback-title">
      <p className="eyebrow">A year with this system</p>
      <h3 id="payback-title">
        {plannerYears != null
          ? `With this system it pays for itself in ${yearsText(plannerYears)}.`
          : 'At these prices the bill savings never repay the system.'}
      </h3>
      {plannerYears != null && selfYears != null && selfYears < plannerYears && (
        <p className="note">
          For this house the battery's default setting would pay back a little sooner, in {yearsText(selfYears)}.
        </p>
      )}

      <div className="payback-grid">
        <article className="card">
          <p className="eyebrow">No solar, no battery</p>
          <p className="payback-years">{money(none)}</p>
          <p className="note">a year, buying all your power from the grid.</p>
        </article>
        <article className="card compare-loss">
          <p className="eyebrow">Battery on its default setting</p>
          <p className="payback-years">{money(payback.annual_bill_self_consumption_aud)}</p>
          <p className="note">
            a year, saving {money(selfSaves)}.{' '}
            {selfYears != null ? `Pays back in ${yearsText(selfYears)}.` : 'Never pays back.'}
          </p>
        </article>
        <article className="card compare-gain">
          <p className="eyebrow">With this system</p>
          <p className="payback-years">{money(payback.annual_bill_planner_aud)}</p>
          <p className="note">
            a year, saving {money(plannerSaves)}.{' '}
            {plannerYears != null ? `Pays back in ${yearsText(plannerYears)}` : 'Never pays back'}
            {sooner != null && sooner > 0.05 ? `, ${yearsText(sooner)} sooner.` : '.'}
          </p>
        </article>
      </div>

      <div className="payback-costs">
        <label>
          <span className="eyebrow">Solar, $ per kW installed</span>
          <input type="number" min="0" step="any" value={solarPerKw} onChange={(e) => setSolarPerKw(e.target.value)} />
        </label>
        <label>
          <span className="eyebrow">Battery, $ per kWh before rebate</span>
          <input
            type="number"
            min="0"
            step="any"
            value={batteryPerKwh}
            onChange={(e) => setBatteryPerKwh(e.target.value)}
          />
        </label>
        <div>
          <p className="eyebrow">System cost</p>
          <p className="payback-years">{money(cost)}</p>
          <p className="note">after a {money(payback.battery_rebate_aud)} federal battery rebate.</p>
        </div>
      </div>

      <p className="note">
        Both battery bills include its wear, at {money(payback.wear_aud_per_kwh)} per kWh moved: about{' '}
        {money(payback.annual_wear_planner_aud)} a year with this system and{' '}
        {money(payback.annual_wear_self_consumption_aud)} on the default setting. This system cycles the battery
        only when the price difference pays for that wear, so it may pay a little more for power and still come
        out ahead.
      </p>
      <p className="note">{payback.basis}</p>
      <p className="note">{payback.cost_sources} Payback ignores price changes, panel ageing and finance costs.</p>
    </section>
  )
}

function positive(raw: string, fallback: number): number {
  const n = Number(raw)
  return Number.isFinite(n) && n >= 0 ? n : fallback
}

function years(cost: number, saving: number): number | null {
  return saving > 0 ? cost / saving : null
}

function yearsText(y: number): string {
  return y < 1 ? 'under a year' : `${y.toFixed(1)} years`
}
