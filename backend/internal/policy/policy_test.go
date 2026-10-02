package policy

import (
	"testing"
	"time"

	"climate-hacktion-curtailment/backend/internal/house"
	"climate-hacktion-curtailment/backend/internal/sim"
	"climate-hacktion-curtailment/backend/internal/split"
)

func TestSelfConsumption(t *testing.T) {
	spec := house.DefaultHouse()
	b := spec.Battery
	cases := []struct {
		name          string
		pv, load, soc float64
		want          sim.Action
	}{
		{"surplus charges", 5, 1, 5, sim.ChargeSurplus},
		{"surplus but full holds", 5, 1, b.SocMaxKwh(), sim.Hold},
		{"deficit discharges", 0, 2, 5, sim.DischargeLoad},
		{"deficit but empty holds", 0, 2, b.SocMinKwh(), sim.Hold},
		{"balanced holds", 2, 2, 5, sim.Hold},
	}
	for _, c := range cases {
		if got := ChooseSelfConsumption(c.pv, c.load, c.soc, spec); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestTariffClock(t *testing.T) {
	spec := house.DefaultHouse()
	at := func(h int) time.Time { return time.Date(2026, 7, 16, h, 0, 0, 0, split.NEM) }
	cases := []struct {
		name          string
		hour          int
		pv, load, soc float64
		want          sim.Action
	}{
		{"night, low battery grid-charges", 23, 0, 0.3, 3, sim.Charge},
		{"night, early morning grid-charges", 3, 0, 0.3, 3, sim.Charge},
		{"night, charged and no sun holds", 2, 0, 0.3, 6, sim.Hold},
		{"peak discharges into load", 18, 0, 2, 6, sim.DischargeLoad},
		{"peak with solar surplus falls back", 17, 3, 1, 6, sim.ChargeSurplus},
		{"midday follows self-consumption", 12, 5, 1, 5, sim.ChargeSurplus},
	}
	for _, c := range cases {
		if got := ChooseTariffClock(c.pv, c.load, c.soc, at(c.hour), spec); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}
