package sim

import (
	"fmt"
	"math"
	"sort"
	"time"

	"climate-hacktion-curtailment/backend/internal/house"
)

// Cash the house pays over a sequence of 5-minute steps.
//
// Energy settles at NSW1 spot, ex-GST, for import and export alike. Still on
// the bill: the daily supply charge plus GST, the two-way adders on the
// tou_fit_twoway tariff, and degradation (AC throughput times the spec's rate).

// EpisodeResult is the total over a run.
type EpisodeResult struct {
	PvAvailKwh            float64
	PvToLoadKwh           float64
	PvToBatteryKwh        float64
	PvToExportKwh         float64
	ClippedKwh            float64
	GridImportKwh         float64
	GridExportKwh         float64
	BatteryChargeAcKwh    float64
	BatteryDischargeAcKwh float64
	BillAud               float64
	ThroughputAcKwh       float64
	SpotMtdAud            float64
}

func (e EpisodeResult) SelfConsumedKwh() float64 { return e.PvToLoadKwh + e.PvToBatteryKwh }

// SpotEnergyAud is the settled energy cash for one step. Positive means the
// house pays. A negative price pays the house to import and charges it to export.
func SpotEnergyAud(step StepResult, priceAudMwh float64) float64 {
	return (step.GridImportKwh - step.GridExportKwh) * priceAudMwh / 1000.0
}

func dayKey(ts time.Time) int { return ts.Year()*10000 + int(ts.Month())*100 + ts.Day() }

// Episode prices a sequence of steps. A nil prices slice prices energy at $0.
func Episode(steps []StepResult, stamps []time.Time, spec house.HouseSpec, prices []float64) (EpisodeResult, error) {
	if len(steps) != len(stamps) {
		return EpisodeResult{}, fmt.Errorf("steps and timestamps must have the same length")
	}
	if prices != nil && len(prices) != len(steps) {
		return EpisodeResult{}, fmt.Errorf("spot prices must align with steps")
	}
	tariff, err := house.GetTariff(spec.TariffID)
	if err != nil {
		return EpisodeResult{}, err
	}

	var res EpisodeResult
	var energy float64
	middayExport := map[int]float64{}
	eveningExport := map[int]float64{}
	days := map[int]struct{}{}

	for i, step := range steps {
		price := 0.0
		if prices != nil {
			price = prices[i]
		}
		energy += SpotEnergyAud(step, price)
		res.PvAvailKwh += step.PvAvailKwh
		res.PvToLoadKwh += step.PvToLoadKwh
		res.PvToBatteryKwh += step.PvToBatteryKwh
		res.PvToExportKwh += step.PvToExportKwh
		res.ClippedKwh += step.ClippedKwh
		res.GridImportKwh += step.GridImportKwh
		res.GridExportKwh += step.GridExportKwh
		res.BatteryChargeAcKwh += step.BatteryChargeAcKwh
		res.BatteryDischargeAcKwh += step.BatteryDischargeAcKwh

		day := dayKey(stamps[i])
		days[day] = struct{}{}
		if house.IsMiddayExportWindow(stamps[i]) {
			middayExport[day] += step.GridExportKwh
		}
		if house.IsEveningExportWindow(stamps[i]) {
			eveningExport[day] += step.GridExportKwh
		}
	}

	supply := float64(len(days)) * tariff.SupplyAudPerDay * (1.0 + tariff.GST)
	twoway := twowayAud(tariff, middayExport, eveningExport)
	res.ThroughputAcKwh = res.BatteryChargeAcKwh + res.BatteryDischargeAcKwh
	degradation := res.ThroughputAcKwh * spec.DegradationAudPerKwh
	res.SpotMtdAud = energy
	res.BillAud = energy + supply + twoway + degradation
	return res, nil
}

func twowayAud(t house.Tariff, midday, evening map[int]float64) float64 {
	if t.MiddayExportCharge == 0 && t.EveningExportCredit == 0 {
		return 0.0
	}
	// Sum in date order so the float result does not depend on map order.
	charge := 0.0
	for _, day := range sortedDays(midday) {
		charge += math.Max(0.0, midday[day]-t.MiddayFreeExportKwh) * t.MiddayExportCharge
	}
	credit := 0.0
	for _, day := range sortedDays(evening) {
		credit += evening[day]
	}
	credit *= t.EveningExportCredit
	return charge - credit
}

func sortedDays(m map[int]float64) []int {
	days := make([]int, 0, len(m))
	for day := range m {
		days = append(days, day)
	}
	sort.Ints(days)
	return days
}
