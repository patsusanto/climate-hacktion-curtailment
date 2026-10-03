export type Action = 'hold' | 'charge_surplus' | 'charge' | 'discharge_load' | 'discharge'

export interface PlaygroundRequest {
  address: string
  pv_kw_ac: number
  battery_kwh: number
  battery_kw?: number
  export_cap_kw?: number
  daily_load_kwh?: number
  window: 'validation' | 'test' | { start: string; end: string }
}

export type PlaygroundEvent =
  | { type: 'meta'; meta: PlaygroundMeta }
  | { type: 'step'; tick: PlaygroundTick }
  | { type: 'done'; summary: PlaygroundSummary }
  | { type: 'error'; message: string }

/** First event. The page can draw the axes before any cash exists. */
export interface PlaygroundMeta {
  assumptions: {
    price_region: 'NSW1'
    price_source: 'historical_spot'
    roof: string
    load: 'evening_peak_synthetic'
    address_label: string
    lat: number
    lon: number
    note: string
    /** How the bills are priced, in one sentence. */
    tariff?: string
  }
  spec: {
    pv_kw_ac: number
    export_cap_kw: number
    battery_kwh: number
    battery_kw: number
    usable_kwh: number
    daily_load_kwh: number
    /** What the planner counts battery wear at, $/kWh moved. Not on the bill. */
    degradation_aud_per_kwh: number
  }
  window: { start: string; end: string; step_minutes: 5; n: number }
}

/**
 * One five-minute step, in order of i.
 * price_est_* are the 1-hour-ahead band aimed at this t, next to the price that occurred.
 * cumulative_savings_aud is self-consumption bill so far minus planner bill so far.
 */
export interface PlaygroundTick {
  i: number
  t: string
  price_aud_mwh: number
  price_est_p10_aud_mwh: number
  price_est_p50_aud_mwh: number
  price_est_p90_aud_mwh: number
  pv_kw: number
  pv_est_kw: number
  load_kw: number
  load_est_kw: number
  action: Action
  soc_kwh: number
  grid_import_kwh: number
  grid_export_kwh: number
  energy_cash_aud: number
  /** Self-consumption energy cost so far. Positive means that strategy has paid out. */
  cumulative_self_aud: number
  cumulative_savings_aud: number
  /** What one kWh cost to import and earned exported this step, $/kWh with the tariff applied. */
  import_aud_kwh?: number
  export_aud_kwh?: number
  /** Why the battery did what it did, in plain words. */
  reason?: string
}

/** Last event. savings_aud matches the last tick. Wear is only known here. */
export interface PlaygroundSummary {
  self_consumption: Bill
  planner: Bill
  savings_aud: number
  savings_with_wear_aud: number
  supply_aud: number
  /** Things to know about this result, e.g. the planner did worse than self-consumption. */
  warnings?: string[]
  /** A year of bills and the system's cost, for the payback figures. */
  payback?: Payback
}

/** All amounts AUD incl. GST, for a year. */
export interface Payback {
  basis: string
  annual_bill_no_system_aud: number
  annual_bill_self_consumption_aud: number
  annual_bill_planner_aud: number
  annual_wear_self_consumption_aud: number
  annual_wear_planner_aud: number
  wear_aud_per_kwh: number
  solar_aud_per_kw: number
  battery_aud_per_kwh: number
  battery_rebate_aud: number
  system_cost_aud: number
  cost_sources: string
  /** 0 when the savings never repay the cost. */
  payback_years_self_consumption: number
  payback_years_planner: number
}

export interface Bill {
  bill_aud: number
  energy_cash_aud: number
  clipped_kwh: number
  throughput_ac_kwh: number
  grid_import_kwh: number
  grid_export_kwh: number
  /** Battery wear at the run's rate; bill_aud includes it. */
  wear_aud?: number
}

/** GET /v1/playground/run/{id}/steps/{i} — not streamed. */
export interface StepDecision {
  t: string
  action: Action
  measured: { price_aud_mwh: number; pv_kw: number; load_kw: number; soc_kwh: number }
  energy_cash_aud: number
  leads: Array<{
    lead_steps: number
    price_p10: number
    price_p50: number
    price_p90: number
    pv_kw: number
    pv_lo: number
    pv_hi: number
    load_kw: number
    load_lo: number
    load_hi: number
  }>
  stories: Array<{
    id: 'mid' | 'bright' | 'dull' | 'spike'
    count: number
    price_aud_mwh: number[]
    pv_kw: number[]
    load_kw: number[]
  }>
}
