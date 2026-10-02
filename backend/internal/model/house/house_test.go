package house

import (
	"math"
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/split"
)

func TestSpecValidation(t *testing.T) {
	if err := DefaultHouse().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := DefaultHouse()
	bad.Battery.CapacityKwh = 0
	if bad.Validate() == nil {
		t.Error("zero capacity should fail")
	}
	bad = DefaultHouse()
	bad.Battery.InitialSocFrac = 0.95
	if bad.Validate() == nil {
		t.Error("initial SOC outside the band should fail")
	}
	bad = DefaultHouse()
	bad.DegradationAudPerKwh = -1
	if bad.Validate() == nil {
		t.Error("negative degradation should fail")
	}
	b := DefaultBattery()
	if math.Abs(b.SocMinKwh()-1) > 1e-12 || math.Abs(b.SocMaxKwh()-9) > 1e-12 {
		t.Errorf("band %v..%v", b.SocMinKwh(), b.SocMaxKwh())
	}
	if math.Abs(b.UsableHours()-1.6) > 1e-12 {
		t.Errorf("usable hours %v", b.UsableHours())
	}
}

func TestTariffBands(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 7, 16, h, m, 0, 0, split.NEM) }
	cases := []struct {
		h, m int
		want string
	}{
		{0, 0, "offpeak"}, {6, 59, "offpeak"}, {7, 0, "shoulder"}, {15, 59, "shoulder"},
		{16, 0, "peak"}, {20, 59, "peak"}, {21, 0, "shoulder"}, {21, 59, "shoulder"}, {22, 0, "offpeak"},
	}
	for _, c := range cases {
		if got := ImportBand(at(c.h, c.m)); got != c.want {
			t.Errorf("%02d:%02d = %s, want %s", c.h, c.m, got, c.want)
		}
	}
	if !IsMiddayExportWindow(at(10, 0)) || IsMiddayExportWindow(at(15, 0)) {
		t.Error("midday window is [10:00, 15:00)")
	}
	if !IsEveningExportWindow(at(16, 0)) || IsEveningExportWindow(at(21, 0)) {
		t.Error("evening window is [16:00, 21:00)")
	}

	tf, err := GetTariff("tou_fit")
	if err != nil {
		t.Fatal(err)
	}
	if ImportAudPerKwh(tf, at(17, 0)) != 0.40 || ImportAudPerKwh(tf, at(12, 0)) != 0.28 || ImportAudPerKwh(tf, at(2, 0)) != 0.18 {
		t.Error("import rates by band")
	}
	if math.Abs(tf.PeakImportInclGST()-0.44) > 1e-12 {
		t.Errorf("peak incl GST %v", tf.PeakImportInclGST())
	}
	tw, _ := GetTariff("tou_fit_twoway")
	if tw.MiddayFreeExportKwh != 6.85 || tw.EveningExportCredit == 0 {
		t.Errorf("two-way adders %+v", tw)
	}
	if _, err := GetTariff("nope"); err == nil {
		t.Error("unknown tariff should fail")
	}
}

func TestLoadBlocksSumToDailyLoad(t *testing.T) {
	total := 0.0
	for hour := 0; hour < 24; hour++ {
		total += LoadBaseKw(hour, 15.0) // kW held for one hour is kWh
	}
	if math.Abs(total-15.0) > 1e-9 {
		t.Errorf("a day of load sums to %v kWh, want 15", total)
	}
}

func TestClearSkyShape(t *testing.T) {
	// Adelaide mid-winter (day 197 is 16 July): dark at midnight, bright at noon, never above 1.
	if v := ClearSkyUnit(0, 0, 0, 197); v != 0 {
		t.Errorf("midnight clear sky %v", v)
	}
	noon := ClearSkyUnit(12, 0, 0, 197)
	if noon < 0.4 || noon > 1 {
		t.Errorf("winter noon clear sky %v", noon)
	}
	summer := ClearSkyUnit(12, 0, 0, 15)
	if summer <= noon {
		t.Errorf("summer noon %v should beat winter noon %v", summer, noon)
	}
	pv, load := ScaleUnits(0.5, 0.3, DefaultHouse())
	if math.Abs(pv-5.25) > 1e-12 || math.Abs(load-0.3) > 1e-12 {
		t.Errorf("scale units %v %v", pv, load)
	}
}
