package battery

import (
	"math"
	"testing"
	"time"
)

var nem = time.FixedZone("NEM", 10*3600)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAusgridPrices(t *testing.T) {
	winterPeak := time.Date(2026, 7, 16, 18, 0, 0, 0, nem) // interval 17:55-18:00
	p := Ausgrid.At(winterPeak, 100)                       // $100/MWh = 10c/kWh
	if want := (0.3252 + 0.10) * 1.1; !near(p.Import, want) || !near(p.Export, 0.10) {
		t.Errorf("winter peak: %+v, want import %v export 0.10", p, want)
	}
	offPeak := time.Date(2026, 7, 16, 12, 0, 0, 0, nem)
	if p := Ausgrid.At(offPeak, 100); !near(p.Import, (0.0536+0.10)*1.1) {
		t.Errorf("off-peak: %+v", p)
	}
	shoulder := time.Date(2026, 4, 16, 18, 0, 0, 0, nem) // April has no peak window
	if p := Ausgrid.At(shoulder, 100); !near(p.Import, (0.0536+0.10)*1.1) {
		t.Errorf("April evening: %+v", p)
	}
	// The interval ending at 15:00 started at 14:55, so it is not yet peak.
	if Ausgrid.Peak(time.Date(2026, 7, 16, 15, 0, 0, 0, nem)) || !Ausgrid.Peak(time.Date(2026, 7, 16, 15, 5, 0, 0, nem)) {
		t.Error("the peak window starts with the interval from 15:00")
	}
	// No GST on a negative spot price, so importing never costs less than exporting earns.
	neg := Ausgrid.At(offPeak, -500)
	if !near(neg.Import, 0.0536*1.1-0.5) || neg.Import < neg.Export {
		t.Errorf("negative price: %+v", neg)
	}
}

// Solar the export cap would clip goes into the battery even when the plan said hold.
func TestSettleStoresSolarThatWouldBeClipped(t *testing.T) {
	spec, _ := NewSpec(6.6, 10, 5, 0, 15, 0) // export cap 0
	p := Prices{Import: 0.2, Export: 0.08}
	s, err := Settle(5, Flows{}, 4, 1, p, spec, ClipEconomic) // plan: do nothing; 3 kW spare solar
	if err != nil {
		t.Fatal(err)
	}
	if s.Clipped > 1e-9 || !near(s.ChargeAC, 3*IntervalHours) || s.Action != ChargeSurplus {
		t.Errorf("clipped %v kWh, charged %v kWh (%s); want the spare solar stored", s.Clipped, s.ChargeAC, s.Action)
	}
	full, err := Settle(spec.SOCMax(), Flows{}, 4, 1, p, spec, ClipEconomic)
	if err != nil {
		t.Fatal(err)
	}
	if !near(full.Clipped, 3*IntervalHours) {
		t.Errorf("a full battery with no export must clip: %+v", full)
	}
}

// At a negative price, solar is clipped rather than exported at a cost.
func TestSettleClipsAtNegativePrices(t *testing.T) {
	spec, _ := NewSpec(6.6, 10, 5, 5, 15, 0)
	s, err := Settle(spec.SOCMax(), Flows{}, 4, 1, Prices{Import: 0.02, Export: -0.05}, spec, ClipEconomic)
	if err != nil {
		t.Fatal(err)
	}
	if s.GridExport > 1e-9 || !near(s.Clipped, 3*IntervalHours) {
		t.Errorf("exported %v, clipped %v", s.GridExport, s.Clipped)
	}
	forced, _ := Settle(spec.SOCMax(), Flows{}, 4, 1, Prices{Import: 0.02, Export: -0.05}, spec, ClipForcedOnly)
	if !near(forced.GridExport, 3*IntervalHours) {
		t.Errorf("forced_only exports anyway: %+v", forced)
	}
}
