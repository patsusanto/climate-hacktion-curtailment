# Battery planning model

Trained models and the Go code that runs them. It forecasts the NSW1 electricity price and a house's solar and demand, plans the battery every 5 minutes, and settles the bill. `cmd/genrun` uses it to write the runs in `backend/runs/`; the API never imports it (see `internal/api/boundary_test.go`).

```
models/      trained models, embedded with go:embed
fetch/       downloads public data: AEMO prices and pre-dispatch, Open-Meteo weather
data/        reads the downloaded CSVs
house/       the simulated house: a Sydney roof and household on observed weather
features/    the models' inputs
xgb/         evaluates the XGBoost price models from their JSON
forecast/    loads and runs both models
planner/     chooses the battery's next 5 minutes from the forecasts
battery/     battery physics and the bill
simulate/    replays a house over a window: planner vs self-consumption
runfile/     turns a replay into the run file the API serves (internal/wire shapes)
synthetic/   a made-up download for tests that must not use the network
```

## Using it

`cmd/genrun` is the normal way in (see the repository README). In code:

```go
start := time.Date(2026, 7, 16, 0, 0, 0, 0, data.NEM)  // market time, UTC+10
end := time.Date(2026, 8, 18, 23, 55, 0, 0, data.NEM)

err := fetch.Days(start, end, "data/validation", false, os.Stderr) // once; a cache

m, err := forecast.Load()
d, err := data.Load("data/validation")
in, err := m.Prepare(d) // builds the house on the data's clock

spec, err := battery.NewSpec(6.6 /* PV kW */, 13.5 /* kWh */, 5 /* kW */, 5 /* export cap kW */, 18 /* kWh/day */, 0 /* wear $/kWh */)
res, err := simulate.Run(m, in, spec, start, end, planner.Economic, nil)
res.Planner.BillAUD, res.Self.BillAUD
detail, err := res.Detail(i) // forecasts and the 8-hour paths the planner used at step i

file, err := runfile.Build(res, runfile.Options{ID: "6.6kw-13.5kwh", WindowName: "validation", DetailEvery: 12, WearAUDPerKWh: 0.05, TrainedBefore: "19 Aug 2026"})
```

A download covers 9 days before `start` and 2 after `end`, because the models need 8 days of history and 8 hours ahead. Pre-dispatch comes in weekly archives of about 125 MB; without it (`skipPredispatch`), the price model is less accurate. Observed weather reaches the archive a few days late, so the most recent days cannot be replayed yet.

## What is inside

| | |
|---|---|
| **Price model** | XGBoost, one model per lead (1, 2, 3, 6 and 8 hours ahead), P10/P50/P90 of the NSW1 price. Inputs: recent prices, the price at the target's time yesterday and last week, day-ahead weather forecasts for Sydney, Dubbo and Goulburn, and AEMO pre-dispatch. |
| **Solar and demand model** | Linear regression on recent solar and demand, clear-sky geometry and day-ahead Sydney weather, at the same leads. |
| **Planner** | Every 5 minutes it finds the cheapest battery plan over the next 8 hours and carries out the first 5 minutes. It plans on the P50 price and the solar/demand forecasts. It reaches the same optimum as the training code's linear program by dynamic programming, so no solver is needed. |
| **The house** | A Sydney roof and an evening-peak household, built from observed weather. Its noise is the training house's (`models/noise_*.bin`), so the same weather gives the same house. |

The models were trained on data before 19 Aug 2026. Over four months they never saw (Dec 2025 - Mar 2026), the price model's error was 27% below the best simple baseline. Over the same months, the planner using these forecasts saved $35.72 more than the same planner using naive forecasts.

**How closely it matches the Python training code:**
- **Forecasts:** identical on all 6,336 steps of 19 Aug - 9 Sep 2026, to the rounding of the output.
- **Planner:** on 60 random 8-hour problems, it reaches the training LP's optimal cost exactly (`go test ./internal/model/planner`). Where two plans cost the same, it can pick a different one than the LP solver did. Over 19 Aug - 9 Sep, 29 of 6,336 steps differ, all with the battery at a limit, and the bill differs by 2 cents.

**Limits:**
- NSW1 only.
- The house is synthetic in shape: real weather, but an assumed demand profile.
- The planner ignores battery wear (0 $/kWh), so it cycles hard. At 5c/kWh of wear, `savings_with_wear_aud` is negative.

## Updating the models

The models are trained in the training repository (`backend-v2`):

```sh
python -m pipeline.run train
python -m pipeline.export --out <path to>/backend/internal/model/models
```

This rewrites `models/`: the price models, the linear model and its feature list in `meta.json`, and the house noise. Then regenerate the runs with `cmd/genrun`.
