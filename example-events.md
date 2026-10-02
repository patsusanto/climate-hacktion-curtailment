# Example playground events

Real output from `POST /v1/playground/run` for the request in `example-body.json`. The data is **placeholder** (synthetic), so the numbers are not real model results, but the shapes are exactly what the service sends.

The frontend sends one request and reads the response body as the stream. Everything below is that stream, with the formatted JSON added for reading. On the wire each event is a single line.

## Request

```sh
curl -N -X POST "localhost:8080/v1/playground/run" \
  -H "Accept: application/x-ndjson, text/event-stream" \
  -H "Content-Type: application/json" \
  -d @example-body.json
```

```json
{
  "address": "1 Example St, Sydney",
  "pv_kw_ac": 10.5,
  "battery_kwh": 10,
  "battery_kw": 5,
  "window": "validation"
}
```

`battery_kw`, `export_cap_kw` and `daily_load_kwh` are optional.

## Response headers

```
HTTP/1.1 200 OK
Content-Type: application/x-ndjson
Cache-Control: no-cache
X-Accel-Buffering: no
X-Run-Id: 10kw-10kwh
Transfer-Encoding: chunked
```

`X-Run-Id` is how the page asks for step detail later (`GET /v1/playground/run/{X-Run-Id}/steps/{i}`).

With `Accept: text/event-stream` the `Content-Type` is `text/event-stream` and each event is wrapped as a `data:` line (see [SSE form](#sse-form)).

## The stream

Every event is one JSON object with a `type`. The order is `meta`, then one `step` per 5 minutes (9,792 here), then `done`.

| Event | Shape |
| --- | --- |
| `meta` | `{"type":"meta","meta":{...}}` |
| `step` | `{"type":"step","tick":{...}}` |
| `done` | `{"type":"done","summary":{...}}` |
| `error` | `{"type":"error","message":"..."}` |

### `meta` (first line)

Raw:

```
{"type":"meta","meta":{"assumptions":{"price_region":"NSW1","price_source":"historical_spot","roof":"synthetic_clear_sky_scaled_by_pv_kw_ac","load":"evening_peak_synthetic","address_label":"1 Example St, Sydney","lat":-33.87,"lon":151.21,"note":"PLACEHOLDER synthetic data from tools/genrun. Solar kilowatts scale the synthetic roof. Demand is the 15 kWh evening-peak shape."},"spec":{"pv_kw_ac":10.5,"export_cap_kw":5,"battery_kwh":10,"battery_kw":5,"usable_kwh":8,"daily_load_kwh":15,"degradation_aud_per_kwh":0},"window":{"start":"2026-07-16T00:00:00+10:00","end":"2026-08-18T23:55:00+10:00","step_minutes":5,"n":9792}}}
```

Formatted `meta`:

```json
{
  "assumptions": {
    "price_region": "NSW1",
    "price_source": "historical_spot",
    "roof": "synthetic_clear_sky_scaled_by_pv_kw_ac",
    "load": "evening_peak_synthetic",
    "address_label": "1 Example St, Sydney",
    "lat": -33.87,
    "lon": 151.21,
    "note": "PLACEHOLDER synthetic data from tools/genrun. Solar kilowatts scale the synthetic roof. Demand is the 15 kWh evening-peak shape."
  },
  "spec": {
    "pv_kw_ac": 10.5,
    "export_cap_kw": 5,
    "battery_kwh": 10,
    "battery_kw": 5,
    "usable_kwh": 8,
    "daily_load_kwh": 15,
    "degradation_aud_per_kwh": 0
  },
  "window": {
    "start": "2026-07-16T00:00:00+10:00",
    "end": "2026-08-18T23:55:00+10:00",
    "step_minutes": 5,
    "n": 9792
  }
}
```

`address_label` is the address from the request. The page uses `window.n` to size the chart and the progress bar.

### `step` (first tick, `i = 0`)

Raw:

```
{"type":"step","tick":{"i":0,"t":"2026-07-16T00:00:00+10:00","price_aud_mwh":80.75,"price_est_p10_aud_mwh":46.05,"price_est_p50_aud_mwh":74.74,"price_est_p90_aud_mwh":110.9,"pv_kw":0,"pv_est_kw":0,"load_kw":0.3,"load_est_kw":0.338,"action":"discharge_load","soc_kwh":3.974,"grid_import_kwh":0,"grid_export_kwh":0,"energy_cash_aud":0,"cumulative_self_aud":0,"cumulative_savings_aud":0}}
```

Formatted `tick`:

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
  "cumulative_self_aud": 0,
  "cumulative_savings_aud": 0
}
```

| Field | Meaning |
| --- | --- |
| `i` | Step index, 0 to `window.n - 1`. |
| `t` | Timestamp of the 5-minute step. |
| `price_aud_mwh` | Spot price that actually occurred. |
| `price_est_p10/p50/p90_aud_mwh` | The 1-hour-ahead forecast aimed at this `t`, so the chart can draw it over the actual price. |
| `pv_kw`, `pv_est_kw` | Solar output, actual and forecast. |
| `load_kw`, `load_est_kw` | Household demand, actual and forecast. |
| `action` | `hold`, `charge_surplus`, `charge`, `discharge_load` or `discharge`. |
| `soc_kwh` | Battery state of charge after this step. |
| `grid_import_kwh`, `grid_export_kwh` | Energy bought from and sold to the grid this step. |
| `energy_cash_aud` | Money spent (positive) or earned (negative) by the planner this step. |
| `cumulative_self_aud` | Self-consumption energy cost so far (positive means paid). New. |
| `cumulative_savings_aud` | Self-consumption cost so far minus planner cost so far. Positive means the planner is ahead. |

The planner's own cost so far is `cumulative_self_aud − cumulative_savings_aud`. The page shows that on the "With this system" card.

### `step` (a tick where the battery sells, `i = 215`)

```json
{
  "i": 215,
  "t": "2026-07-16T17:55:00+10:00",
  "price_aud_mwh": 200.8,
  "price_est_p10_aud_mwh": 138.57,
  "price_est_p50_aud_mwh": 198.09,
  "price_est_p90_aud_mwh": 277.42,
  "pv_kw": 0,
  "pv_est_kw": 0,
  "load_kw": 1.655,
  "load_est_kw": 1.453,
  "action": "discharge",
  "soc_kwh": 6.255,
  "grid_import_kwh": 0,
  "grid_export_kwh": 0.2787,
  "energy_cash_aud": -0.056,
  "cumulative_self_aud": -1.1081,
  "cumulative_savings_aud": 0.056
}
```

In the evening peak the planner sells to the grid: it exports 0.2787 kWh at 200.8 AUD/MWh, so this step earns 0.056 AUD (a negative `energy_cash_aud`).

### `done` (last line)

Raw:

```
{"type":"done","summary":{"planner":{"bill_aud":-26.59,"energy_cash_aud":-26.59,"clipped_kwh":60.72,"throughput_ac_kwh":552.11,"grid_import_kwh":164.93,"grid_export_kwh":745.91},"savings_aud":9.32,"savings_with_wear_aud":9.05,"self_consumption":{"bill_aud":-17.28,"energy_cash_aud":-17.28,"clipped_kwh":59.51,"throughput_ac_kwh":546.82,"grid_import_kwh":98.76,"grid_export_kwh":681.22},"supply_aud":37.4}}
```

Formatted `summary`:

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
- `savings_aud` is the self-consumption bill minus the planner bill, here −17.28 minus −26.59, about 9.32. It matches `cumulative_savings_aud` on the last tick.
- `savings_with_wear_aud` is the same figure after battery wear is counted. It is only known at the end.
- `supply_aud` is the supply charge for the window. The page says the bills include it. In this placeholder file `bill_aud` equals `energy_cash_aud`, so it does not. Real engine output does include supply.

### `error`

The page understands an `error` event and shows its message, and it treats the stream as failed. The service does not send one on this route today, because the runs are precomputed and cannot fail part-way. It is here for when the model runs live. The shape is:

```
{"type":"error","message":"the model failed at step 4200"}
```

A bad request never starts a stream. It is a plain JSON body with status 400, which the page also shows:

```json
{ "message": "address is required" }
```

## SSE form

With `Accept: text/event-stream` (and not NDJSON) each event is a single `data:` line followed by a blank line. There are no `id:` or `event:` lines, because the page parses every line it receives.

```
data: {"type":"meta","meta":{ ... }}

data: {"type":"step","tick":{"i":0, ... }}

data: {"type":"step","tick":{"i":1, ... }}
```

## Step detail

Loaded when a step is selected, with the id from `X-Run-Id`. Not streamed.

```sh
curl localhost:8080/v1/playground/run/10kw-10kwh/steps/12
```

```json
{
  "t": "2026-07-16T01:00:00+10:00",
  "action": "discharge_load",
  "measured": { "price_aud_mwh": 83.82, "pv_kw": 0, "load_kw": 0.336, "soc_kwh": 3.611 },
  "energy_cash_aud": 0,
  "leads": [
    { "lead_steps": 1, "price_p10": 49.52, "price_p50": 68.8, "price_p90": 86.78, "pv_kw": 0, "pv_lo": 0, "pv_hi": 0, "load_kw": 0.337, "load_lo": 0.269, "load_hi": 0.404 },
    "... 10 more ...",
    { "lead_steps": 12, "price_p10": 37.27, "price_p50": 72.83, "price_p90": 157.32, "pv_kw": 0, "pv_lo": 0, "pv_hi": 0, "load_kw": 0.335, "load_lo": 0.173, "load_hi": 0.624 }
  ],
  "stories": [
    {
      "id": "mid",
      "count": 12,
      "price_aud_mwh": [68.15, 72.09, 75.93, 102.89, 70.97, 80.97, 81.24, 69.58, 72, 75.94, 66.01, 81.89],
      "pv_kw": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
      "load_kw": [0.337, 0.346, 0.351, 0.315, 0.317, 0.307, 0.368, 0.296, 0.31, 0.335, 0.366, 0.335]
    },
    "... bright, dull and spike, each with the same three arrays ..."
  ]
}
```

- `t` must equal the tick's `t`. The page ignores a detail whose `t` does not match.
- Only every 12th step has detail in this placeholder run; others return 404.
- The placeholder has 12 leads and story counts of 12, 4, 4 and 1. The real model has 5 leads (1, 2, 3, 6 and 8 hours) and story counts of 7, 5, 5 and 4.

## The older two-step flow

`POST` without a streaming `Accept` header returns `{"run_id": "...", "meta": {...}}` as JSON. Then `GET /v1/playground/run/{run_id}/events` streams Server-Sent Events as `id:`, `event:` and a raw `data:` per step:

```
id: 0
event: step
data: {"i":0,"t":"2026-07-16T00:00:00+10:00", ... ,"cumulative_self_aud":0,"cumulative_savings_aud":0}
```

This route is kept for tools that already use it. The frontend does not.
