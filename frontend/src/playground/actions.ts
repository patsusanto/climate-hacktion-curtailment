import type { Action } from './types'

export const actionLabel: Record<Action, string> = {
  hold: 'Hold',
  charge_surplus: 'Charge surplus',
  charge: 'Charge',
  discharge_load: 'Discharge to load',
  discharge: 'Discharge',
}

/** Lane and chip fills. */
export const actionFill: Record<Action, string> = {
  hold: 'rgb(27 47 74 / 14%)',
  charge_surplus: 'var(--blue)',
  charge: 'var(--rose)',
  discharge_load: 'var(--gain)',
  discharge: 'var(--amber)',
}

export const actionOrder: Action[] = [
  'hold',
  'charge_surplus',
  'charge',
  'discharge_load',
  'discharge',
]
