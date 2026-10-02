package house

import (
	"fmt"
	"time"
)

// Synthetic retail tariffs. These are not a retailer offer.
//
// Import and the daily supply charge take 10% GST. Feed-in is credited ex-GST.
// Two-way export adders are ex-GST and apply only on tou_fit_twoway.
// Times passed in must already be on the market clock (UTC+10).

const (
	GST                 = 0.10
	FitAudPerKwh        = 0.05
	SupplyAudPerDay     = 1.00 // 100 c/day
	MiddayExportCharge  = 0.012320
	EveningExportCredit = 0.038551
	MiddayFreeExportKwh = 6.85
	tariffTouFit        = "tou_fit"
	tariffTouFitTwoWay  = "tou_fit_twoway"
)

type Tariff struct {
	ID                  string
	PeakAudPerKwh       float64
	ShoulderAudPerKwh   float64
	OffpeakAudPerKwh    float64
	FitAudPerKwh        float64
	SupplyAudPerDay     float64
	GST                 float64
	MiddayExportCharge  float64
	MiddayFreeExportKwh float64
	EveningExportCredit float64
}

func touFit() Tariff {
	return Tariff{
		ID:                tariffTouFit,
		PeakAudPerKwh:     0.40,
		ShoulderAudPerKwh: 0.28,
		OffpeakAudPerKwh:  0.18,
		FitAudPerKwh:      FitAudPerKwh,
		SupplyAudPerDay:   SupplyAudPerDay,
		GST:               GST,
	}
}

// GetTariff looks a tariff up by id.
func GetTariff(id string) (Tariff, error) {
	switch id {
	case tariffTouFit:
		return touFit(), nil
	case tariffTouFitTwoWay:
		t := touFit()
		t.ID = tariffTouFitTwoWay
		t.MiddayExportCharge = MiddayExportCharge
		t.MiddayFreeExportKwh = MiddayFreeExportKwh
		t.EveningExportCredit = EveningExportCredit
		return t, nil
	}
	return Tariff{}, fmt.Errorf("unknown tariff_id %q", id)
}

func (t Tariff) PeakImportInclGST() float64 { return t.PeakAudPerKwh * (1.0 + t.GST) }

func minuteOfDay(ts time.Time) int { return ts.Hour()*60 + ts.Minute() }

// ImportBand is "peak" [16:00, 21:00), "shoulder" [07:00, 16:00) and
// [21:00, 22:00), otherwise "offpeak".
func ImportBand(ts time.Time) string {
	m := minuteOfDay(ts)
	switch {
	case 16*60 <= m && m < 21*60:
		return "peak"
	case (7*60 <= m && m < 16*60) || (21*60 <= m && m < 22*60):
		return "shoulder"
	}
	return "offpeak"
}

func IsMiddayExportWindow(ts time.Time) bool {
	m := minuteOfDay(ts)
	return 10*60 <= m && m < 15*60
}

func IsEveningExportWindow(ts time.Time) bool {
	m := minuteOfDay(ts)
	return 16*60 <= m && m < 21*60
}

// ImportAudPerKwh is the ex-GST import rate at ts.
func ImportAudPerKwh(t Tariff, ts time.Time) float64 {
	switch ImportBand(ts) {
	case "peak":
		return t.PeakAudPerKwh
	case "shoulder":
		return t.ShoulderAudPerKwh
	}
	return t.OffpeakAudPerKwh
}
