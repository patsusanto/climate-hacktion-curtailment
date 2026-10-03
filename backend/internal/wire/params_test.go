package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func good() PlaygroundRequest {
	return PlaygroundRequest{Address: "1 Example St", PvKwAc: 6.6, BatteryKwh: 13.5, Window: json.RawMessage(`"validation"`)}
}

func TestResolveFillsInDefaults(t *testing.T) {
	p, err := good().Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.BatteryKw != 5 || p.ExportCapKw != 5 || p.DailyLoadKwh != 15 || p.Window != "validation" || p.Address != "1 Example St" {
		t.Errorf("%+v", p)
	}
	r := good()
	r.BatteryKw, r.ExportCapKw, r.DailyLoadKwh = f(3), f(0), f(18)
	r.Address = "  padded  "
	p, err = r.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.BatteryKw != 3 || p.ExportCapKw != 0 || p.DailyLoadKwh != 18 || p.Address != "padded" {
		t.Errorf("explicit values and trimming: %+v", p)
	}
}

func TestResolveRejectsBadRequests(t *testing.T) {
	cases := map[string]struct {
		change func(*PlaygroundRequest)
		want   string
	}{
		"no address":     {func(r *PlaygroundRequest) { r.Address = "  " }, "address is required"},
		"long address":   {func(r *PlaygroundRequest) { r.Address = strings.Repeat("a", 201) }, "too long"},
		"zero solar":     {func(r *PlaygroundRequest) { r.PvKwAc = 0 }, "pv_kw_ac must be greater than 0"},
		"huge solar":     {func(r *PlaygroundRequest) { r.PvKwAc = 101 }, "pv_kw_ac must be at most 100"},
		"zero battery":   {func(r *PlaygroundRequest) { r.BatteryKwh = 0 }, "battery_kwh must be greater than 0"},
		"huge battery":   {func(r *PlaygroundRequest) { r.BatteryKwh = 201 }, "battery_kwh must be at most 200"},
		"negative power": {func(r *PlaygroundRequest) { r.BatteryKw = f(-1) }, "battery_kw must be greater than 0"},
		"zero power":     {func(r *PlaygroundRequest) { r.BatteryKw = f(0) }, "battery_kw must be greater than 0"},
		"negative cap":   {func(r *PlaygroundRequest) { r.ExportCapKw = f(-1) }, "export_cap_kw must not be negative"},
		"zero load":      {func(r *PlaygroundRequest) { r.DailyLoadKwh = f(0) }, "daily_load_kwh must be greater than 0"},
		"huge load":      {func(r *PlaygroundRequest) { r.DailyLoadKwh = f(500) }, "daily_load_kwh must be at most 200"},
		"no window":      {func(r *PlaygroundRequest) { r.Window = nil }, "window must be"},
		"bad window":     {func(r *PlaygroundRequest) { r.Window = json.RawMessage(`"nope"`) }, `window must be "summer", "validation" or "test"`},
		"custom window":  {func(r *PlaygroundRequest) { r.Window = json.RawMessage(`{"start":"a","end":"b"}`) }, "custom windows are not supported"},
	}
	for name, c := range cases {
		r := good()
		c.change(&r)
		if _, err := r.Resolve(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error containing %q", name, err, c.want)
		}
	}
}

func TestLimitsAreInclusive(t *testing.T) {
	r := good()
	r.PvKwAc, r.BatteryKwh = MaxSolarKw, MaxBatteryKwh
	r.BatteryKw, r.ExportCapKw, r.DailyLoadKwh = f(MaxBatteryKw), f(MaxExportCapKw), f(MaxDailyLoadKwh)
	if _, err := r.Resolve(); err != nil {
		t.Errorf("the largest allowed house must pass: %v", err)
	}
}

func TestIDRoundTrips(t *testing.T) {
	p := Params{PvKwAc: 6.6, BatteryKwh: 13.5, BatteryKw: 5, ExportCapKw: 5, DailyLoadKwh: 18, Window: "validation"}
	id := p.ID()
	if id != "pv6.6-b13.5-bp5-ec5-l18-validation" {
		t.Errorf("id %q", id)
	}
	back, err := ParseID(id)
	if err != nil || back != p {
		t.Errorf("round trip: %+v, %v", back, err)
	}
	// the address is not part of the id
	a, b := p, p
	a.Address, b.Address = "one", "two"
	if a.ID() != b.ID() {
		t.Error("the address must not change the id")
	}
	// any decimal survives, including ones with a long fraction
	for _, v := range []float64{0.1, 3.3, 7.25, 12.345678, 99.99} {
		q := p
		q.PvKwAc = v
		if back, err := ParseID(q.ID()); err != nil || back.PvKwAc != v {
			t.Errorf("%v: %v (%v)", v, back.PvKwAc, err)
		}
	}
}

func TestParseIDRejectsJunk(t *testing.T) {
	for _, id := range []string{
		"", "10kw-10kwh", "pv6.6-b13.5-bp5-ec5-l18", "pv6.6-b13.5-bp5-ec5-l18-validation-extra",
		"xx6.6-b13.5-bp5-ec5-l18-validation", "pv6.6-b13.5-bp5-ec5-l18-nope",
		"pvabc-b13.5-bp5-ec5-l18-validation", "pv1e1-b13.5-bp5-ec5-l18-validation",
		"pv0-b13.5-bp5-ec5-l18-validation", "pv6.6-b999-bp5-ec5-l18-validation", "pv6.6-b13.5-bp5-ec-1-l18-validation",
	} {
		if _, err := ParseID(id); err == nil {
			t.Errorf("%q should not parse", id)
		}
	}
}
