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
	// ForecastSteps is the 8 hours the forecasts cover, in 5-minute steps.
	ForecastSteps    = 96
	passthroughGuard = 1e-5 // $/kWh on battery throughput
	forcedClipExtra  = 1e-4 // $/kWh on clipping in forced_only mode
)

// Curtail is how the plan may clip solar.
type Curtail = battery.Clip

const (
	Economic   = battery.ClipEconomic
	ForcedOnly = battery.ClipForcedOnly
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

// step is one horizon step: prices in $/kWh, energy in kWh, and the most the battery can move
// and the house can export in it (a step can stand for several 5-minute intervals).
type step struct {
	price     battery.Prices
	pv, load  float64
	pMax, cap float64
}

type problem struct {
	curtail Curtail
	eta     float64 // one leg's efficiency
	w       float64 // $/kWh of throughput
}

// bRange is the battery's AC energy range for the step (+ charge, - discharge).
func (p *problem) bRange(s step) (lo, hi float64) {
	if s.price.Export < 0 {
		return 0, s.pMax // no discharging at a negative price
	}
	return -math.Min(s.pMax, s.load+s.cap), s.pMax // a discharge can cover the load and fill the cap
}

// cost of the step when the battery moves b kWh (AC), with solar clipped or exported at best.
func (p *problem) cost(s step, b float64) float64 {
	_, c := battery.GridNet(b, s.pv, s.load, s.cap, s.price, p.curtail)
	if p.curtail == ForcedOnly { // clipping that the cap forces costs what exporting would, and a little
		c += (math.Max(-s.price.Export, 0) + forcedClipExtra) * math.Max(s.pv-s.load-b-s.cap, 0)
	}
	return c + p.w*math.Abs(b)
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

// stepCost is the step's cost as a convex function of the SOC change. The cost is linear in b
// between the points where the battery, the solar and the export cap change roles.
func (p *problem) stepCost(s step) *pwl {
	lo, hi := p.bRange(s)
	bs := []float64{lo, hi, 0, s.pv - s.load - s.cap, s.pv - s.load, -s.load}
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
	out := &pwl{x0: v.x0 - he, y0: v.y0 + h.eval(he), length: make([]float64, 0, len(v.slope)+n), slope: make([]float64, 0, len(v.slope)+n)}
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
	out := &pwl{x0: lo, y0: f.eval(lo), length: make([]float64, 0, len(f.length)), slope: make([]float64, 0, len(f.length))}
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

// Horizon is what the planner plans on, one entry per 5-minute step from now: the prices
// ($/kWh) and the PV and load (kW). Step 0 is the interval about to start.
type Horizon struct {
	Price    []battery.Prices
	PV, Load []float64
	// Intervals is how many 5-minute intervals each step stands for (its prices, PV and load are
	// the averages over them). Nil means one each.
	Intervals []int
}

// Plan is the planner's answer.
type Plan struct {
	First battery.Flows // what to do in the next 5 minutes
	// The whole plan: battery AC energy per step (kWh, + charge) and the charge after each step.
	Battery, SOC []float64
	// Start is when each step starts, in 5-minute intervals from now.
	Start []int
	Cost  float64 // the plan's cost over the horizon ($, wear included)
}

// Solve finds the cheapest plan from charge soc (kWh). The battery's wear (spec.WearAUDPerKWh)
// is part of the cost, so it cycles only when the price difference pays for the wear.
func Solve(soc float64, h Horizon, spec battery.Spec, curtail Curtail) (Plan, error) {
	if curtail != Economic && curtail != ForcedOnly {
		return Plan{}, fmt.Errorf("curtail must be %q or %q", Economic, ForcedOnly)
	}
	n := len(h.Price)
	if n == 0 || len(h.PV) != n || len(h.Load) != n {
		return Plan{}, fmt.Errorf("planner: the horizon's prices, PV and load must have the same length")
	}
	if h.Intervals != nil && len(h.Intervals) != n {
		return Plan{}, fmt.Errorf("planner: the horizon's intervals must match its steps")
	}
	p := &problem{curtail: curtail, eta: spec.Leg(), w: spec.WearAUDPerKWh + passthroughGuard}
	steps := make([]step, n)
	costs := make([]*pwl, n)
	start := make([]int, n)
	at0 := 0
	for k := range steps {
		m := 1
		if h.Intervals != nil {
			m = max(h.Intervals[k], 1)
		}
		dt := float64(m) * battery.IntervalHours
		steps[k] = step{price: h.Price[k], pv: math.Max(h.PV[k], 0) * dt, load: math.Max(h.Load[k], 0) * dt,
			pMax: spec.MaxPowerKW * dt, cap: spec.ExportCapKW * dt}
		costs[k] = p.stepCost(steps[k])
		start[k] = at0
		at0 += m
	}
	lo, hi := spec.SOCMin(), spec.SOCMax()
	soc = math.Min(math.Max(soc, lo), hi)

	// Backwards: v[k] is the cheapest cost of steps k..n-1 as a function of the charge before k.
	v := make([]*pwl, n+1)
	v[n] = &pwl{x0: lo, y0: 0, length: []float64{hi - lo}, slope: []float64{0}} // nothing after the horizon
	for k := n - 1; k >= 1; k-- {
		v[k] = restrict(infConv(v[k+1], costs[k]), lo, hi)
	}

	// Forwards: at each step take the best move given the cost-to-go after it.
	plan := Plan{Battery: make([]float64, n), SOC: make([]float64, n), Start: start}
	at := soc
	for k := 0; k < n; k++ {
		d, c := best(costs[k], v[k+1], at, lo, hi)
		if k == 0 {
			plan.Cost = c
		}
		plan.Battery[k] = p.ac(d)
		at = math.Min(math.Max(at+d, lo), hi)
		plan.SOC[k] = at
	}
	s := steps[0]
	b := plan.Battery[0]
	if math.Abs(b) < 1e-12 {
		b = 0
	}
	net, _ := battery.GridNet(b, s.pv, s.load, s.cap, s.price, curtail)
	plan.First = battery.Route(b, net, s.pv, s.load)
	return plan, nil
}

// best minimises h(d) + v(soc + d) over the charge changes the step allows. The sum is convex
// and piecewise linear, so it walks the breakpoints from the left until the slope turns
// non-negative. Where the minimum is flat, it takes the smallest move.
func best(h, v *pwl, soc, lo, hi float64) (d, cost float64) {
	const eps = 1e-12
	dLo, dHi := math.Max(h.x0, lo-soc), math.Min(h.end(), hi-soc)
	if dHi < dLo {
		dHi = dLo
	}
	hs, vs := newCursor(h, dLo), newCursor(v, soc+dLo)
	x := dLo
	for x < dHi-eps && hs.slope()+vs.slope() < -eps {
		x = math.Min(dHi, math.Min(hs.next(), vs.next()-soc))
		hs.seek(x)
		vs.seek(soc + x)
	}
	left := x
	for x < dHi-eps && hs.slope()+vs.slope() <= eps { // a flat bottom: find where it ends
		x = math.Min(dHi, math.Min(hs.next(), vs.next()-soc))
		hs.seek(x)
		vs.seek(soc + x)
	}
	d = math.Min(math.Max(0, left), math.Max(left, x)) // 0 if the flat bottom [left, x] holds it
	return d, h.eval(d) + v.eval(soc+d)
}

// cursor walks a pwl's segments left to right.
type cursor struct {
	f  *pwl
	k  int     // segment index
	at float64 // where segment k starts
}

func newCursor(f *pwl, x float64) *cursor {
	c := &cursor{f: f, at: f.x0}
	c.seek(x)
	return c
}

// seek moves to the segment that continues right of x.
func (c *cursor) seek(x float64) {
	for c.k < len(c.f.length) && c.at+c.f.length[c.k] <= x+1e-15 {
		c.at += c.f.length[c.k]
		c.k++
	}
}

// slope right of the cursor (0 past the end).
func (c *cursor) slope() float64 {
	if c.k >= len(c.f.slope) {
		return 0
	}
	return c.f.slope[c.k]
}

// next is the end of the current segment.
func (c *cursor) next() float64 {
	if c.k >= len(c.f.length) {
		return math.Inf(1)
	}
	return c.at + c.f.length[c.k]
}

// Path is ForecastSteps (8 hours) from the value now through the forecasts at the leads (linear between).
func Path(now float64, leads []int, values []float64) []float64 {
	knots := append([]int{0}, leads...)
	ys := append([]float64{now}, values...)
	out := make([]float64, ForecastSteps)
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
