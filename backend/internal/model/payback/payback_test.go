package payback

import (
	"math"
	"testing"
)

func TestRebateTiers(t *testing.T) {
	for _, c := range []struct{ kWh, want float64 }{
		{10, 10 * 272},
		{14, 14 * 272},
		{20, 14*272 + 6*272*0.6},
		{30, 14*272 + 14*272*0.6 + 2*272*0.15},
		{60, 14*272 + 14*272*0.6 + 22*272*0.15}, // nothing above 50 kWh
	} {
		if got := Rebate(c.kWh); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Rebate(%g) = %v, want %v", c.kWh, got, c.want)
		}
	}
}

func TestCostAndYears(t *testing.T) {
	total, rebate := Cost(6.6, 13.5)
	if want := 6.6*SolarAUDPerKW + 13.5*BatteryAUDPerKWh - rebate; math.Abs(total-want) > 1e-9 {
		t.Errorf("cost %v, want %v", total, want)
	}
	if Years(10000, 1000) != 10 || Years(10000, 0) != 0 || Years(10000, -5) != 0 {
		t.Error("years")
	}
}
