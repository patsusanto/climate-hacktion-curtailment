# Planner statistics

How much the battery planner saves, measured on 3,000 simulated sites over data the model never saw. This is an analysis tool. It isn't part of the deployed services, and nothing else imports it.

## Results

1,000 sites of each type. Each is a random 4-week period between 1 Dec 2025 and 18 Sep 2026. The model was trained on data before 1 Dec 2025, so it never saw any of these periods. Battery wear is included in every result.

| | House | Industrial | Utility |
|---|---|---|---|
| Typical site | 8 kW solar, 14 kWh battery | 211 kW solar, 258 kWh battery | 26 MW solar, 27 MWh battery |
| Beat the battery's default setting | 78% of runs | 92% | 98% |
| Median gain vs default | 2.6% lower bill (~$26 a year) | 9.9% lower bill (~$5,900 a year) | 11.6% more revenue (~$168k a year) |
| Median vs no solar or battery | 61% lower bill | 46% lower bill | +7.1% revenue vs solar alone |

- **Best season:** summer gains are largest (businesses and solar farms about 16%, homes about 3% of their bill).
- **Main exception:** homes that can't export, where the planner wins only 20% of the time.

**Caveats:**

- **Industrial demand is simulated:** a daytime business profile with random hours, weekends and air-conditioning. Demand charges aren't modelled.
- **The utility "default" is a fixed schedule:** charge 10 am – 3 pm, sell 5–9 pm. That's simpler than a professional trader, so the gain against real operations is smaller. The comparison with solar alone is like-for-like.
- **The model was trained once and not updated.** A live system retrained regularly would forecast later months better.
- **Annual figures are 4-week results × 365/28.** There's no spring, because the data ends in September.
- **For houses, use the medians.** Bills near zero distort means and percentages.

## The sites

| | House | Industrial | Utility |
|---|---|---|---|
| Demand | The model's household profile, 8–30 kWh a day | Daytime business profile, 300–3,000 kWh a day; the planner forecasts it as "same time last week" | None |
| Solar | 3–13 kW | 0.05–0.25 kW per kWh of daily demand | 5–50 MW |
| Battery | 5–20 kWh | 0.05–0.3 kWh per kWh of daily demand, 2-hour | 25–100% of solar MW, 1–2 hours |
| Export limit | 0 / 1.5 / 5 / 10 kW | 0 / 50 / 100% of solar | 80–100% of solar (grid connection) |
| Tariff | Ausgrid EA025 residential + spot | Ausgrid EA225 business + spot | Spot price (wholesale) |
| Default (the comparison) | Battery's own setting: store spare solar, run the house from it | Same | Fixed daily schedule |
| Battery wear | 5c/kWh moved | 4c/kWh | 3c/kWh |

## Rerunning it

From `backend/`:

```sh
go run ./analysis/stats -n 1000 -out analysis/stats/results/stats_1000.csv
```

- **Time:** about 7 minutes. It prints the summary and writes one CSV row per run (sizes, demand shape, and the cost under each strategy).
- **Data:** it reads `backend/data/year`.
- **Reproducible:** the same `-seed` (default 1) gives the same sites and byte-identical results, so the CSV is not committed (`results/` is git-ignored).
