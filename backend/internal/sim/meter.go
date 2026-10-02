package sim

import (
	"time"

	"climate-hacktion-curtailment/backend/internal/house"
)

// Meter steps a house through a trace and settles the episode bill.
type Meter struct {
	Spec    house.HouseSpec
	SocKwh  float64
	history []StepResult
	stamps  []time.Time
	prices  []float64
}

func NewMeter(spec house.HouseSpec) *Meter {
	m := &Meter{Spec: spec}
	m.Reset()
	return m
}

func (m *Meter) Reset() {
	m.SocKwh = m.Spec.Battery.InitialSocKwh()
	m.history = m.history[:0]
	m.stamps = m.stamps[:0]
	m.prices = m.prices[:0]
}

// Step settles one interval and advances the state of charge.
func (m *Meter) Step(ts time.Time, action Action, pvKw, loadKw, priceAudMwh float64) (StepResult, error) {
	res, err := Apply(m.SocKwh, action, pvKw, loadKw, m.Spec)
	if err != nil {
		return StepResult{}, err
	}
	m.SocKwh = res.SocKwh
	m.history = append(m.history, res)
	m.stamps = append(m.stamps, ts)
	m.prices = append(m.prices, priceAudMwh)
	return res, nil
}

// Finish prices the whole run.
func (m *Meter) Finish() (EpisodeResult, error) {
	return Episode(m.history, m.stamps, m.Spec, m.prices)
}
