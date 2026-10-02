# Example playground events

Real events from `GET /v1/playground/run/10kw-10kwh/events` (placeholder data).

On the wire, each event is `id`, `event`, `data`, then a blank line, and `data` is a single line of JSON. The formatted JSON below is for reading only.

## `step` event (first tick, `i = 0`)

Raw:

```
id: 0
event: step
data: {"i":0,"t":"2026-07-16T00:00:00+10:00","price_aud_mwh":80.75,"price_est_p10_aud_mwh":46.05,"price_est_p50_aud_mwh":74.74,"price_est_p90_aud_mwh":110.9,"pv_kw":0,"pv_est_kw":0,"load_kw":0.3,"load_est_kw":0.338,"action":"discharge_load","soc_kwh":3.974,"grid_import_kwh":0,"grid_export_kwh":0,"energy_cash_aud":0,"cumulative_savings_aud":0}
```

Formatted `data`:

```json
{
  "i": 0,
  "t": "2026-07-16T00:00:00+10:00",
  "price_aud_mwh": 80.75,
  "price_est_p10_aud_mwh": 46.05,
  "price_est_p50_aud_mwh": 74.74,
  "price_est_p90_aud_mwh": 110.9,
  "pv_kw": 0,
  "pv_est_kw": 0,
  "load_kw": 0.3,
  "load_est_kw": 0.338,
  "action": "discharge_load",
  "soc_kwh": 3.974,
  "grid_import_kwh": 0,
  "grid_export_kwh": 0,
  "energy_cash_aud": 0,
  "cumulative_savings_aud": 0
}
```

| Field | Meaning |
| --- | --- |
| `i` | Step index, 0 to 9791. Also the SSE `id`. |
| `t` | Timestamp of the 5-minute step. |
| `price_aud_mwh` | Spot price that actually occurred. |
| `price_est_p10/p50/p90_aud_mwh` | The 1-hour-ahead forecast band aimed at this `t`. |
| `pv_kw`, `pv_est_kw` | Solar output, actual and forecast. |
| `load_kw`, `load_est_kw` | Household demand, actual and forecast. |
| `action` | `hold`, `charge_surplus`, `charge`, `discharge_load` or `discharge`. |
| `soc_kwh` | Battery state of charge. |
| `grid_import_kwh`, `grid_export_kwh` | Energy bought from and sold to the grid this step. |
| `energy_cash_aud` | Money spent or earned this step. |
| `cumulative_savings_aud` | Self-consumption bill so far minus planner bill so far. Positive means the planner is ahead. |

## `done` event (last event, `id = 9791`)

Raw:

```
id: 9791
event: done
data: {"planner":{"bill_aud":-26.59,"energy_cash_aud":-26.59,"clipped_kwh":60.72,"throughput_ac_kwh":552.11,"grid_import_kwh":164.93,"grid_export_kwh":745.91},"savings_aud":9.32,"savings_with_wear_aud":9.05,"self_consumption":{"bill_aud":-17.28,"energy_cash_aud":-17.28,"clipped_kwh":59.51,"throughput_ac_kwh":546.82,"grid_import_kwh":98.76,"grid_export_kwh":681.22},"supply_aud":37.4}
```

Formatted `data`:

```json
{
  "planner": {
    "bill_aud": -26.59,
    "energy_cash_aud": -26.59,
    "clipped_kwh": 60.72,
    "throughput_ac_kwh": 552.11,
    "grid_import_kwh": 164.93,
    "grid_export_kwh": 745.91
  },
  "savings_aud": 9.32,
  "savings_with_wear_aud": 9.05,
  "self_consumption": {
    "bill_aud": -17.28,
    "energy_cash_aud": -17.28,
    "clipped_kwh": 59.51,
    "throughput_ac_kwh": 546.82,
    "grid_import_kwh": 98.76,
    "grid_export_kwh": 681.22
  },
  "supply_aud": 37.4
}
```

- A negative `bill_aud` means the household earned more from exports than it spent on imports.
- `savings_aud` is the self-consumption bill minus the planner bill, here -17.28 minus -26.59, about 9.32.
- `savings_with_wear_aud` is the same figure after battery wear is counted. It is only known at the end.
- `supply_aud` is the supply charge for the window.
