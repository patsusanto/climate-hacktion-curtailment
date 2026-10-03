# Battery planning model

Trained models and the Go code that runs them. It forecasts the NSW1 electricity price and a house's solar and demand, plans the battery every 5 minutes, settles the bill on a real retail tariff, explains each decision, and estimates a year of bills and the system's payback. `cmd/genrun` and the worker (`internal/worker`) use it; the public API never imports it (see `internal/api/boundary_test.go`).

```
models/      trained models, embedded with go:embed
fetch/       downloads public data: AEMO prices and pre-dispatch, Open-Meteo weather
data/        reads the downloaded CSVs (plain or gzipped); the named windows and the payback year
house/       the simulated house: a Sydney roof and household on observed weather
features/    the models' inputs
xgb/         evaluates the XGBoost price models from their JSON
forecast/    loads and runs both models (once per data file; houses share the forecasts)
battery/     the tariff, battery physics and the bill
planner/     chooses the battery's next 5 minutes: the cheapest plan over the next 24 hours
simulate/    replays a house (planner vs self-consumption), the reason for each step, the year
payback/     system costs, the federal battery rebate, years to pay back
runfile/     turns a replay into the run the API serves (internal/wire shapes)
synthetic/   a made-up download for tests that must not use the network
```

## Using it

`cmd/genrun` and the worker are the normal ways in (see the repository README). In code:

```go
w := data.Windows["validation"] // first and last interval, NEM time (UTC+10)
m, err := forecast.Load()
d, err := data.Load("data/validation")
in, err := m.Prepare(d) // builds the house on the data's clock

spec, err := battery.NewSpec(6.6 /* PV kW */, 13.5 /* kWh */, 5 /* kW */, 5 /* export cap kW */, 18 /* kWh/day */, 0)
opt := simulate.DefaultOptions() // Ausgrid tariff, 5c/kWh wear, 24-hour plan
res, err := simulate.Run(m, in, spec, w[0], w[1], opt, nil)
res.Steps[i].Reason // "Saving the charge for 6:30 pm, when power is forecast at 48c/kWh (now 17c/kWh)."

yd, err := data.Load("data/year")
year, err := m.Prepare(yd)
annual, err := simulate.RunAnnual(m, year, spec, data.Year[0], data.Year[1], simulate.AnnualWeeks, opt)
res.Annual = &annual // adds the payback figures to the run

file, err := runfile.Build(res, runfile.Options{ID: "6.6kw-13.5kwh", WindowName: "validation", DetailEvery: 12, TrainedBefore: m.TrainedBefore()})
```

## How a house is billed

The house is on a spot pass-through plan (like Amber) on Ausgrid's network:

- **Import:** the NSW1 spot price plus Ausgrid's EA025 network charge, plus GST. The network charge is 32.52c/kWh from 3 to 9 pm in summer (Nov–Mar) and winter (Jun–Aug), and 5.36c/kWh at other times, both ex GST. It's in `battery.Ausgrid`.
- **Export:** earns the spot price, which can be negative.
- **Supply:** $1.10 a day.
- **Not included:** the retailer's own per-kWh fees and environmental charges.
- **Battery wear:** 5c per kWh moved through the battery (AC, charging and discharging) is part of every bill shown, for both ways of running the battery. That's about $800/kWh spread over roughly 6,000 cycles. Without it, a strategy that cycles the battery harder would look better than it is.

## How the battery is run

- **Planner:** every 5 minutes it finds the cheapest plan for the next 24 hours, counting the tariff and the wear, and carries out the first 5 minutes.
  - For the first 8 hours it uses the forecasts: the P50 price, plus solar and demand.
  - For hours 8–24 it assumes tomorrow repeats today, in hourly steps. This is enough to see that tomorrow's sun will refill the battery, so it stops buying power overnight that free solar would have provided.
  - It solves the same problem as the training code's linear program, by dynamic programming. On 60 reference problems it reaches the LP's optimal cost exactly (`go test ./internal/model/planner`).
- **Controller** (`battery.Settle`): it follows the plan's charge or discharge within the battery's limits.
  - It buys from the grid or exports from the battery only if the plan meant to.
  - It decides clipping from the interval's actual price.
  - It always stores solar that would otherwise be clipped if the battery has room, even when the forecast missed it.
- **Self-consumption**, the battery's default setting and the baseline: it stores spare solar and covers the house from the battery, ignoring prices.
- **Reasons:** each step's reason comes from the plan it acted on. It says what the stored energy is for and when, or why it is buying, selling, holding or clipping.

**How it compares with self-consumption,** with wear counted for both (dollars saved over the window; negative means self-consumption did better). These are the page's houses (export cap 5 kW). Every window is data the model never saw:

| House (PV kW / battery kWh / load kWh a day) | Summer, 1 Dec – 28 Feb | Winter, 16 Jul – 18 Aug | Test, 19 Aug – 9 Sep |
|---|---|---|---|
| 3.3 / 6.5 / 12 (Small roof) | +47.35 | −0.18 | +0.64 |
| 6.6 / 13.5 / 18 (Family home) | +47.99 | −0.30 | +1.01 |
| 10 / 27 / 30 (All-electric) | +67.44 | −1.59 | +0.45 |
| 10.5 / 10 / 15 (default) | | +2.24 | |

- **Summer:** most of the value is here. Self-consumption keeps exporting at negative midday prices and the planner doesn't, and it times the evening peak.
- **Winter:** there's little to gain, and the forecasts for it are the oldest (eight months past the training data), so the planner roughly breaks even.

The planner's old setup (spot prices only, no wear, 8 hours ahead) lost to self-consumption in every house tested once wear was counted. The changes behind the improvement:

- the real tariff;
- wear in the plan;
- the 24-hour look-ahead;
- storing solar that would be clipped.

A fallback that hands a step to self-consumption when the plan's expected gain is small was also tried. It cost slightly more than it saved, so it's off (`Options.FallbackAUD`). These choices were made by looking at the winter and test windows, so summer is the cleanest evidence for them.

**When it loses:** houses that can't export, or with a small battery for their solar and demand. The planner has little to work with there, so the run's summary carries a warning whenever self-consumption came out ahead.

## One model, shown only on data it never saw

There is one trained model, fitted on data before 1 Dec 2025 (`TRAIN_CUTOFF` in the training repo). Everything the playground replays comes after that date: the three windows and the payback year. No window is in the model's training data.

The price model's error (pinball loss; lower is better) on each window:

| Window | Error | Average miss on the P50 price |
|---|---|---|
| Summer, Dec 2025 – Feb 2026 | 13.46 | $33.70/MWh |
| Winter, 16 Jul – 18 Aug 2026 | 5.88 | $18.10/MWh |
| Test, 19 Aug – 9 Sep 2026 | 5.28 | $16.10/MWh |

Summer's error is higher because summer prices are more volatile, not because the model is worse there.

**The trade-off is staleness.** A model forecasts best just after its training data, and gets worse as the market moves on. A model trained on data up to 19 Aug 2026 scored 4.03 on the same test window, against this model's 5.28. A live system would be retrained regularly, say monthly, and each period forecast by the latest model before it. This playground uses one model so every result is cleanly out of sample, and the later months are somewhat pessimistic as a result.

**Remaining caveat:** the training pipeline chose its features and settings on Apr–Sep 2026, and the planner's changes were chosen on the winter and test windows. Those windows influenced the design, though not the model's weights. Summer influenced neither.

## Payback

`RunAnnual` replays 14 weeks, one every three weeks from 1 Dec 2025 to 18 Sep 2026, and scales them to a year. That span runs from where the model's training data ends to where the AEMO pre-dispatch data ends, so it's all unseen. It covers 10 months; October and November are missing and are assumed to be like the rest. It produces three annual bills: the house with no solar and no battery (all demand from the grid), self-consumption, and the planner. Both battery bills include wear.

`payback` prices the system: solar at $900/kW installed after the STC rebate, and the battery at $800/kWh before the federal Cheaper Home Batteries rebate. That rebate is $272/kWh at the May–Dec 2026 rate: in full for the first 14 kWh, 60% from 14 to 28 kWh, and 15% from 28 to 50 kWh. Payback years are the system cost divided by the yearly saving against no solar and no battery. The page lets people enter their own prices.

For the page's houses, the planner pays back about a year sooner than self-consumption: 7.3 vs 8.3 years for the Small roof, 9.2 vs 10.2 for the Family home, and 11.2 vs 11.9 for the All-electric house.

Treat payback as an estimate:

- **A few events drive it.** Most of the planner's yearly gain comes from summer, when it avoids exporting at negative prices, and from a handful of price spikes. Spikes can swing a sampled week by $20 either way.
- **October and November are missing.** Spring often has negative midday prices, so the missing months probably understate the planner's advantage.
- **It is simple payback.** It ignores price changes, panel ageing and finance costs.

## What is inside

| | |
|---|---|
| **Price model** | XGBoost, one model per lead (1, 2, 3, 6 and 8 hours ahead), P10/P50/P90 of the NSW1 price. Inputs: recent prices, the price at the target's time yesterday and last week, day-ahead weather forecasts for Sydney, Dubbo and Goulburn, and AEMO pre-dispatch. |
| **Solar and demand model** | Linear regression on recent solar and demand, clear-sky geometry and day-ahead Sydney weather, at the same leads. |
| **The house** | A Sydney roof and an evening-peak household, built from observed weather. Its noise is the training house's (`models/noise_*.bin`), so the same weather gives the same house. |

The model was trained on data before 1 Dec 2025. Over the four months after that (Dec 2025 – Mar 2026), the price model's error was 27% below the best simple baseline. The Go forecasts match the Python training code to the rounding of the output.

**Data:** `backend/data/validation` and `test` are the replay windows. `backend/data/year` (gzipped, 3.8 MB) is the payback year, built from the same AEMO and Open-Meteo sources as `fetch`; where it overlaps the validation window, every value matches the fetched data.

**Speed:** one core replays a 34-day window in about 2 seconds, and the 13 payback weeks in about 5. Forecasts are computed once per data file, and the worker runs the window and the year in parallel.

**Limits:**
- NSW1 and Ausgrid only.
- The house is synthetic in shape: real weather, but an assumed demand profile.
- The export cap is one fixed kW limit. Dynamic ("flexible") export limits aren't modelled.

## Updating the models

The models are trained in the training repository (`backend-v2`):

```sh
python -m pipeline.run train
python -m pipeline.export --out <path to>/backend/internal/model/models
```

This rewrites `models/`: the price models, the linear model and its feature list in `meta.json`, and the house noise. Then regenerate the runs with `cmd/genrun`.
