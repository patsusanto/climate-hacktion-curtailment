package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The largest house the playground runs. The model only checks signs, so these limits are what
// keep one request from asking for something absurd, and they apply wherever a request enters.
const (
	MaxAddressLen   = 200
	MaxSolarKw      = 100.0
	MaxBatteryKwh   = 200.0
	MaxBatteryKw    = 100.0
	MaxExportCapKw  = 100.0
	MaxDailyLoadKwh = 200.0
)

// What is used when an optional field is left out.
const (
	DefaultBatteryKw    = 5.0
	DefaultExportCapKw  = 5.0
	DefaultDailyLoadKwh = 15.0
)

// Params is a request that has been checked, with its defaults filled in.
type Params struct {
	Address      string // only a label; the series does not depend on it
	PvKwAc       float64
	BatteryKwh   float64
	BatteryKw    float64
	ExportCapKw  float64
	DailyLoadKwh float64
	Window       string // one of Windows
}

// Resolve checks the request and fills in the defaults. The error text is meant for the user.
func (r PlaygroundRequest) Resolve() (Params, error) {
	address := strings.TrimSpace(r.Address)
	switch {
	case address == "":
		return Params{}, errors.New("address is required")
	case utf8.RuneCountInString(address) > MaxAddressLen:
		return Params{}, errors.New("address is too long")
	}
	p := Params{
		Address:      address,
		PvKwAc:       r.PvKwAc,
		BatteryKwh:   r.BatteryKwh,
		BatteryKw:    DefaultBatteryKw,
		ExportCapKw:  DefaultExportCapKw,
		DailyLoadKwh: DefaultDailyLoadKwh,
	}
	if r.BatteryKw != nil {
		p.BatteryKw = *r.BatteryKw
	}
	if r.ExportCapKw != nil {
		p.ExportCapKw = *r.ExportCapKw
	}
	if r.DailyLoadKwh != nil {
		p.DailyLoadKwh = *r.DailyLoadKwh
	}
	if err := p.checkHouse(); err != nil {
		return Params{}, err
	}
	window, err := windowName(r.Window)
	if err != nil {
		return Params{}, err
	}
	p.Window = window
	return p, nil
}

// checkHouse checks the sizes (not the address or the window).
func (p Params) checkHouse() error {
	for _, c := range []struct {
		name      string
		v, max    float64
		allowZero bool
	}{
		{"pv_kw_ac", p.PvKwAc, MaxSolarKw, false},
		{"battery_kwh", p.BatteryKwh, MaxBatteryKwh, false},
		{"battery_kw", p.BatteryKw, MaxBatteryKw, false},
		{"export_cap_kw", p.ExportCapKw, MaxExportCapKw, true},
		{"daily_load_kwh", p.DailyLoadKwh, MaxDailyLoadKwh, false},
	} {
		switch {
		case c.allowZero && !(c.v >= 0):
			return fmt.Errorf("%s must not be negative", c.name)
		case !c.allowZero && !(c.v > 0):
			return fmt.Errorf("%s must be greater than 0", c.name)
		case c.v > c.max:
			return fmt.Errorf("%s must be at most %g", c.name, c.max)
		}
	}
	return nil
}

// Windows are the replay windows a request can name. Their dates are in the model's data
// package: summer is 1 Dec 2025 - 28 Feb 2026, validation 16 Jul - 18 Aug 2026 (winter), and
// test 19 Aug - 9 Sep 2026 (the only one the models never saw).
var Windows = []string{"summer", "validation", "test"}

// KnownWindow says whether name is one of Windows.
func KnownWindow(name string) bool {
	for _, w := range Windows {
		if w == name {
			return true
		}
	}
	return false
}

const windowChoices = `"summer", "validation" or "test"`

// windowName accepts one of Windows. Custom {start, end} windows are not supported because the
// data for a window has to be prepared in advance.
func windowName(raw json.RawMessage) (string, error) {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		if KnownWindow(name) {
			return name, nil
		}
		return "", errors.New("window must be " + windowChoices)
	}
	var custom struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if json.Unmarshal(raw, &custom) == nil && custom.Start != "" && custom.End != "" {
		return "", errors.New("custom windows are not supported yet; use " + windowChoices)
	}
	return "", errors.New("window must be " + windowChoices)
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// ID names a run by its parameters, for example "pv6.6-b13.5-bp5-ec5-l18-validation". It is
// the same for the same house and window, so any server can find the run again from the id alone.
// The address is not part of it.
func (p Params) ID() string {
	return "pv" + num(p.PvKwAc) + "-b" + num(p.BatteryKwh) + "-bp" + num(p.BatteryKw) +
		"-ec" + num(p.ExportCapKw) + "-l" + num(p.DailyLoadKwh) + "-" + p.Window
}

// ParseID is the inverse of ID. An id is untrusted input, so the sizes are checked again.
func ParseID(id string) (Params, error) {
	parts := strings.Split(id, "-")
	if len(parts) != 6 {
		return Params{}, fmt.Errorf("%q is not a run id", id)
	}
	var p Params
	for i, field := range []struct {
		prefix string
		dst    *float64
	}{{"pv", &p.PvKwAc}, {"b", &p.BatteryKwh}, {"bp", &p.BatteryKw}, {"ec", &p.ExportCapKw}, {"l", &p.DailyLoadKwh}} {
		rest, ok := strings.CutPrefix(parts[i], field.prefix)
		if !ok {
			return Params{}, fmt.Errorf("%q is not a run id", id)
		}
		v, err := strconv.ParseFloat(rest, 64)
		if err != nil || strings.ContainsAny(rest, "eE+") {
			return Params{}, fmt.Errorf("%q is not a run id", id)
		}
		*field.dst = v
	}
	if !KnownWindow(parts[5]) {
		return Params{}, fmt.Errorf("%q is not a run id", id)
	}
	p.Window = parts[5]
	if err := p.checkHouse(); err != nil {
		return Params{}, err
	}
	return p, nil
}

// House is the same parameters without the address, which is only a label. Two requests for the
// same house and window have equal Houses.
func (p Params) House() Params {
	p.Address = ""
	return p
}

// Request is a request that resolves to these parameters, with nothing left to default.
func (p Params) Request() PlaygroundRequest {
	battery, cap, load := p.BatteryKw, p.ExportCapKw, p.DailyLoadKwh
	window, _ := json.Marshal(p.Window)
	return PlaygroundRequest{
		Address: p.Address, PvKwAc: p.PvKwAc, BatteryKwh: p.BatteryKwh,
		BatteryKw: &battery, ExportCapKw: &cap, DailyLoadKwh: &load, Window: window,
	}
}
