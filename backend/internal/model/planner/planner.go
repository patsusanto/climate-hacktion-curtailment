// Package planner chooses the battery's next 5 minutes from 8-hour forecasts.
//
// Objective (one house, minimised over the next 8 hours, re-solved every 5 minutes, only the
// first 5 minutes applied):
//
//	sum_k  price_k x (import_k - export_k)  +  wear x battery throughput_k
//	subject to PV and load balance, battery power and SOC limits (10-90%),
//	90% round-trip efficiency, and the export cap.
//
// This is the training code's linear program, solved exactly by dynamic programming: with one
// battery, each step's best cost is a convex piecewise-linear function of the change in charge,
// so the cost-to-go is too, and it is carried backwards through the horizon as a list of slopes
// (each step is an infimal convolution, a merge of two sorted slope lists). No LP solver is needed.
//
// As in the LP: a tiny throughput cost makes charging and discharging in one step pointless;
// discharging at a negative price is ruled out; clipping solar is a free choice when exporting
// would cost money (curtail "economic"), or costs what exporting would plus a little, so only
// the cap and a full battery force it ("forced_only").
package planner

import (
	"fmt"
	"math"
	"sort"

	"climate-hacktion-curtailment/backend/internal/model/battery"
)

const (
	// HorizonSteps is 8 hours of 5-minute steps.
	HorizonSteps     = 96
	passthroughGuard = 1e-5 // $/kWh on battery throughput
	forcedClipExtra  = 1e-4 // $/kWh on clipping in forced_only mode
)

// Curtail is how the plan may clip solar.
type Curtail string

const (
	Economic   Curtail = "economic"
	ForcedOnly Curtail = "forced_only"
)

// pwl is a convex piecewise-linear function on [x0, x0 + sum(len)]: its value at x0, then
// segments in increasing slope.
type pwl struct {
	x0, y0 float64
	length []float64
	slope  []float64
}

func (f *pwl) end() float64 {
	x := f.x0
	for _, l := range f.length {
		x += l
	}
	return x
}

// eval at x (clamped to the domain).
func (f *pwl) eval(x float64) float64 {
	y, at := f.y0, f.x0
	for k, l := range f.length {
		if x <= at+l {
			return y + f.slope[k]*math.Max(x-at, 0)
		}
		y += f.slope[k] * l
		at += l
	}
	return y
}

// knots returns the breakpoints and their values.
func (f *pwl) knots() (xs, ys []float64) {
	xs, ys = []float64{f.x0}, []float64{f.y0}
	x, y := f.x0, f.y0
	for k, l := range f.length {
		x += l
		y += f.slope[k] * l
		xs, ys = append(xs, x), append(ys, y)
	}
	return xs, ys
}

// hull builds the lower convex hull through points sorted by x.
func hull(xs, ys []float64) *pwl {
	var hx, hy []float64
	for i := range xs {
		if len(hx) > 0 && xs[i]-hx[len(hx)-1] < 1e-12 {
			if ys[i] < hy[len(hy)-1] {
				hy[len(hy)-1] = ys[i]
			}
			continue
		}
		for len(hx) >= 2 {
			a, b := len(hx)-2, len(hx)-1
			// drop b if it lies on or above the line from a to i
			if (hy[b]-hy[a])*(xs[i]-hx[a]) >= (ys[i]-hy[a])*(hx[b]-hx[a]) {
				hx, hy = hx[:b], hy[:b]
				continue
			}
			break
		}
		hx, hy = append(hx, xs[i]), append(hy, ys[i])
	}
	f := &pwl{x0: hx[0], y0: hy[0]}
	for k := 1; k < len(hx); k++ {
		l := hx[k] - hx[k-1]
		f.length = append(f.length, l)
		f.slope = append(f.slope, (hy[k]-hy[k-1])/l)
	}
	return f
}

// step is one horizon step's inputs in kWh and $/kWh.
type step struct {
	spot, pv, load float64
}

type problem struct {
	spec    battery.Spec
	curtail Curtail
	eta     float64 // one leg's efficiency
	pMax    float64 // kWh per step
	cap     float64 // kWh per step
	w       float64 // $/kWh of throughput
}

// bRange is the battery's AC energy range for the step (+ charge, - discharge).
func (p *problem) bRange(s step) (lo, hi float64) {
	if s.spot < 0 {
		return 0, p.pMax // no discharging at a negative price
	}
	return -math.Min(p.pMax, s.load+p.cap), p.pMax // a discharge can cover the load and fill the cap
}

// cost of the step's grid flows when the battery moves b kWh (AC), the flows chosen optimally.
func (p *problem) cost(s step, b float64) float64 {
	wear := p.w * math.Abs(b)
	if s.spot < 0 && p.curtail == Economic {
		return s.spot*(s.load+b) + wear // clip all solar, buy the load and the charge, and be paid
	}
	x := s.load - s.pv + b // net demand on the grid
	if x >= -p.cap {
		return s.spot*x + wear
	}
	clip := 0.0
	if p.curtail == ForcedOnly {
		clip = math.Max(-s.spot, 0) + forcedClipExtra
	}
	return -s.spot*p.cap + clip*(-x-p.cap) + wear
}

// soc change for an AC energy b, and back.
func (p *problem) delta(b float64) float64 {
	if b > 0 {
		return b * p.eta
	}
	return b / p.eta
}

func (p *problem) ac(d float64) float64 {
	if d > 0 {
		return d / p.eta
	}
	return d * p.eta
}

// stepCost is the step's cost as a convex function of the SOC change.
func (p *problem) stepCost(s step) *pwl {
	lo, hi := p.bRange(s)
	bs := []float64{lo, hi, 0, -(s.load - s.pv) - p.cap}
	sort.Float64s(bs)
	var xs, ys []float64
	for _, b := range bs {
		if b < lo || b > hi {
			continue
		}
		xs = append(xs, p.delta(b))
		ys = append(ys, p.cost(s, b))
	}
	return hull(xs, ys)
}

// infConv is (V □ g)(s) = min_{s'} V(s') + g(s - s'), for g(t) = h(-t): the best cost from
// charge s when the step moves it by d at cost h(d) and V is the cost-to-go after.
func infConv(v, h *pwl) *pwl {
	// g's segments: h's reversed, slopes negated; g starts at -end(h).
	n := len(h.slope)
	gl, gs := make([]float64, n), make([]float64, n)
	for k := 0; k < n; k++ {
		gl[k] = h.length[n-1-k]
		gs[k] = -h.slope[n-1-k]
	}
	he := h.end()
	out := &pwl{x0: v.x0 - he, y0: v.y0 + h.eval(he)}
	i, j := 0, 0
	for i < len(v.slope) || j < n {
		if j == n || (i < len(v.slope) && v.slope[i] <= gs[j]) {
			out.length, out.slope = append(out.length, v.length[i]), append(out.slope, v.slope[i])
			i++
		} else {
			out.length, out.slope = append(out.length, gl[j]), append(out.slope, gs[j])
			j++
		}
	}
	return out
}

// restrict cuts f to [lo, hi], merging segments of equal slope.
func restrict(f *pwl, lo, hi float64) *pwl {
	out := &pwl{x0: lo, y0: f.eval(lo)}
	at := f.x0
	for k, l := range f.length {
		a, b := math.Max(at, lo), math.Min(at+l, hi)
		at += l
		if b-a <= 1e-15 {
			continue
		}
		if m := len(out.slope); m > 0 && math.Abs(out.slope[m-1]-f.slope[k]) < 1e-15 {
			out.length[m-1] += b - a
			continue
		}
		out.length, out.slope = append(out.length, b-a), append(out.slope, f.slope[k])
	}
	return out
}

// Plan returns the first step's flows for the horizon (prices $/MWh, PV and load kW).
func Plan(soc float64, price, pvKW, loadKW []float64, spec battery.Spec, curtail Curtail) (battery.Flows, error) {
	f, _, err := solve(soc, price, pvKW, loadKW, spec, curtail)
	return f, err
}

// solve returns the first step's flows and the horizon's optimal cost ($).
func solve(soc float64, price, pvKW, loadKW []float64, spec battery.Spec, curtail Curtail) (battery.Flows, float64, error) {
	if curtail != Economic && curtail != ForcedOnly {
		return battery.Flows{}, 0, fmt.Errorf("curtail must be %q or %q", Economic, ForcedOnly)
	}
	n := len(price)
	p := &problem{spec: spec, curtail: curtail, eta: spec.Leg(), pMax: spec.MaxPowerKW * battery.IntervalHours,
		cap: spec.ExportCapKW * battery.IntervalHours, w: spec.WearAUDPerKWh + passthroughGuard}
	steps := make([]step, n)
	for k := range steps {
		steps[k] = step{spot: price[k] / 1000, pv: math.Max(pvKW[k], 0) * battery.IntervalHours, load: math.Max(loadKW[k], 0) * battery.IntervalHours}
	}
	lo, hi := spec.SOCMin(), spec.SOCMax()
	soc = math.Min(math.Max(soc, lo), hi)

	v := &pwl{x0: lo, y0: 0, length: []float64{hi - lo}, slope: []float64{0}} // nothing after the horizon
	for k := n - 1; k >= 1; k-- {
		v = restrict(infConv(v, p.stepCost(steps[k])), lo, hi)
	}

	// The first step: minimise h(d) + V(soc + d) over its breakpoints and V's.
	h := p.stepCost(steps[0])
	dLo, dHi := math.Max(h.x0, lo-soc), math.Min(h.end(), hi-soc)
	cands := []float64{dLo, dHi, 0}
	hx, _ := h.knots()
	vx, _ := v.knots()
	cands = append(cands, hx...)
	for _, x := range vx {
		cands = append(cands, x-soc)
	}
	best, bestCost := 0.0, math.Inf(1)
	for _, d := range cands {
		if d < dLo-1e-12 || d > dHi+1e-12 {
			continue
		}
		d = math.Min(math.Max(d, dLo), dHi)
		c := h.eval(d) + v.eval(soc+d)
		if c < bestCost-1e-12 || (math.Abs(c-bestCost) <= 1e-12 && math.Abs(d) < math.Abs(best)) {
			best, bestCost = d, c
		}
	}
	return p.flows(steps[0], p.ac(best)), bestCost, nil
}

// flows spells out the first step for an AC battery energy b, the way the LP would route it.
func (p *problem) flows(s step, b float64) battery.Flows {
	if math.Abs(b) < 1e-12 {
		b = 0
	}
	var f battery.Flows
	if s.spot < 0 && p.curtail == Economic {
		f.PVClipped, f.GridToLoad, f.GridToBattery = s.pv, s.load, math.Max(b, 0)
		return f
	}
	if b >= 0 {
		f.PVToLoad = math.Min(s.pv, s.load)
		residual := s.pv - f.PVToLoad
		f.PVToBattery = math.Min(b, residual)
		f.GridToBattery = b - f.PVToBattery
		f.GridToLoad = s.load - f.PVToLoad
		f.PVToExport = math.Min(residual-f.PVToBattery, p.cap)
		f.PVClipped = residual - f.PVToBattery - f.PVToExport
		return f
	}
	d := -b
	unmet := math.Max(s.load-s.pv, 0)
	f.BatteryToExport = math.Min(math.Max(d-unmet, 0), p.cap)
	f.BatteryToLoad = d - f.BatteryToExport
	f.PVToLoad = math.Min(s.pv, s.load-f.BatteryToLoad)
	f.GridToLoad = s.load - f.PVToLoad - f.BatteryToLoad
	f.PVToExport = math.Min(s.pv-f.PVToLoad, math.Max(p.cap-f.BatteryToExport, 0))
	f.PVClipped = s.pv - f.PVToLoad - f.PVToExport
	return f
}

// Path is 96 steps from the value now through the forecasts at the leads (linear between).
func Path(now float64, leads []int, values []float64) []float64 {
	knots := append([]int{0}, leads...)
	ys := append([]float64{now}, values...)
	out := make([]float64, HorizonSteps)
	for i := range out {
		k := sort.SearchInts(knots, i)
		switch {
		case k < len(knots) && knots[k] == i:
			out[i] = ys[k]
		case k >= len(knots):
			out[i] = ys[len(ys)-1]
		default:
			w := float64(i-knots[k-1]) / float64(knots[k]-knots[k-1])
			out[i] = ys[k-1] + w*(ys[k]-ys[k-1])
		}
	}
	return out
}
