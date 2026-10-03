// Package wire holds the JSON shapes of the playground, as defined in
// interfacespec.md: the request, the run metadata, the streamed ticks and
// summary, and the per-step forecast detail.
//
// It is plain data. It has no logic and imports nothing, so both the API
// server and the model code can use it without depending on each other.
package wire

import "encoding/json"

// PlaygroundRequest is the body of POST /v1/playground/run. Window is
// "validation", "test" or {"start", "end"}, so it is kept raw.
type PlaygroundRequest struct {
	Address      string          `json:"address"`
	PvKwAc       float64         `json:"pv_kw_ac"`
	BatteryKwh   float64         `json:"battery_kwh"`
	BatteryKw    *float64        `json:"battery_kw"`
	ExportCapKw  *float64        `json:"export_cap_kw"`
	DailyLoadKwh *float64        `json:"daily_load_kwh"`
	Window       json.RawMessage `json:"window"`
}

// Meta describes a run. It is sent once, in the response to the start request.
type Meta struct {
	Assumptions Assumptions `json:"assumptions"`
	Spec        Spec        `json:"spec"`
	Window      Window      `json:"window"`
}

type Assumptions struct {
	PriceRegion  string  `json:"price_region"`
	PriceSource  string  `json:"price_source"`
	Roof         string  `json:"roof"`
	Load         string  `json:"load"`
	AddressLabel string  `json:"address_label"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	Note         string  `json:"note"`
	// Tariff says how the bills are priced, in one sentence.
	Tariff string `json:"tariff,omitempty"`
}

type Spec struct {
	PvKwAc               float64 `json:"pv_kw_ac"`
	ExportCapKw          float64 `json:"export_cap_kw"`
	BatteryKwh           float64 `json:"battery_kwh"`
	BatteryKw            float64 `json:"battery_kw"`
	UsableKwh            float64 `json:"usable_kwh"`
	DailyLoadKwh         float64 `json:"daily_load_kwh"`
	DegradationAudPerKwh float64 `json:"degradation_aud_per_kwh"`
}

type Window struct {
	Start       string `json:"start"`
	End         string `json:"end"`
	StepMinutes int    `json:"step_minutes"`
	N           int    `json:"n"`
}

// Tick is one 5-minute step (PlaygroundTick).
type Tick struct {
	I             int     `json:"i"`
	T             string  `json:"t"`
	PriceAudMwh   float64 `json:"price_aud_mwh"`
	PriceEstP10   float64 `json:"price_est_p10_aud_mwh"`
	PriceEstP50   float64 `json:"price_est_p50_aud_mwh"`
	PriceEstP90   float64 `json:"price_est_p90_aud_mwh"`
	PvKw          float64 `json:"pv_kw"`
	PvEstKw       float64 `json:"pv_est_kw"`
	LoadKw        float64 `json:"load_kw"`
	LoadEstKw     float64 `json:"load_est_kw"`
	Action        string  `json:"action"`
	SocKwh        float64 `json:"soc_kwh"`
	GridImportKwh float64 `json:"grid_import_kwh"`
	GridExportKwh float64 `json:"grid_export_kwh"`
	EnergyCashAud float64 `json:"energy_cash_aud"`
	// CumulativeSelfAud is the self-consumption energy cost so far (positive means
	// that strategy has paid). The planner's cost so far is CumulativeSelfAud - CumulativeSavingsAud.
	CumulativeSelfAud    float64 `json:"cumulative_self_aud"`
	CumulativeSavingsAud float64 `json:"cumulative_savings_aud"`
	// CumulativeSupplyAud is the daily supply charge accrued so far, the same on both bills.
	CumulativeSupplyAud float64 `json:"cumulative_supply_aud"`
	// What one kWh cost to import and earned exported this step ($/kWh, the tariff applied).
	ImportAudKwh float64 `json:"import_aud_kwh"`
	ExportAudKwh float64 `json:"export_aud_kwh"`
	// Reason says in plain words why the battery did what it did.
	Reason string `json:"reason,omitempty"`
}

// Bill is one policy's totals.
type Bill struct {
	BillAud         float64 `json:"bill_aud"`
	EnergyCashAud   float64 `json:"energy_cash_aud"`
	ClippedKwh      float64 `json:"clipped_kwh"`
	ThroughputAcKwh float64 `json:"throughput_ac_kwh"`
	GridImportKwh   float64 `json:"grid_import_kwh"`
	GridExportKwh   float64 `json:"grid_export_kwh"`
	// WearAud is battery wear at the run's rate per kWh moved. BillAud includes it, so the two
	// ways of running the battery compare like for like.
	WearAud float64 `json:"wear_aud"`
}

// Summary is the last event of the stream (PlaygroundSummary).
type Summary struct {
	SelfConsumption    Bill    `json:"self_consumption"`
	Planner            Bill    `json:"planner"`
	SavingsAud         float64 `json:"savings_aud"`
	SavingsWithWearAud float64 `json:"savings_with_wear_aud"`
	SupplyAud          float64 `json:"supply_aud"`
	// Warnings are things the user should know about this result, such as the planner doing
	// worse than self-consumption for this house.
	Warnings []string `json:"warnings,omitempty"`
	// Payback is what the system costs and how fast it pays for itself, from a year of replay.
	Payback *Payback `json:"payback,omitempty"`
}

// Payback compares a year of bills and the system's cost. All amounts are AUD, incl. GST.
type Payback struct {
	// Basis says what year was replayed and how.
	Basis string `json:"basis"`
	// Bills for a year: the house with no solar and no battery, the same house with this system
	// running self-consumption, and with it run by the planner.
	AnnualBillNoSystemAud        float64 `json:"annual_bill_no_system_aud"`
	AnnualBillSelfConsumptionAud float64 `json:"annual_bill_self_consumption_aud"`
	AnnualBillPlannerAud         float64 `json:"annual_bill_planner_aud"`
	// Battery wear for a year at WearAudPerKwh, each way of running it.
	AnnualWearSelfConsumptionAud float64 `json:"annual_wear_self_consumption_aud"`
	AnnualWearPlannerAud         float64 `json:"annual_wear_planner_aud"`
	WearAudPerKwh                float64 `json:"wear_aud_per_kwh"`
	// Installed cost defaults, before and after rebates; the page lets the user change them.
	SolarAudPerKw    float64 `json:"solar_aud_per_kw"`
	BatteryAudPerKwh float64 `json:"battery_aud_per_kwh"`
	BatteryRebateAud float64 `json:"battery_rebate_aud"`
	SystemCostAud    float64 `json:"system_cost_aud"`
	CostSources      string  `json:"cost_sources"`
	// Years for the bill savings (against no solar and no battery) to repay the system cost.
	// Zero when the system never pays back.
	PaybackYearsSelfConsumption float64 `json:"payback_years_self_consumption"`
	PaybackYearsPlanner         float64 `json:"payback_years_planner"`
}

// Lead is one lead of the forecast issued at a step.
type Lead struct {
	LeadSteps int     `json:"lead_steps"`
	PriceP10  float64 `json:"price_p10"`
	PriceP50  float64 `json:"price_p50"`
	PriceP90  float64 `json:"price_p90"`
	PvKw      float64 `json:"pv_kw"`
	PvLo      float64 `json:"pv_lo"`
	PvHi      float64 `json:"pv_hi"`
	LoadKw    float64 `json:"load_kw"`
	LoadLo    float64 `json:"load_lo"`
	LoadHi    float64 `json:"load_hi"`
}

// Story is one of the four forecast stories over the planning grid.
type Story struct {
	ID          string    `json:"id"`
	Count       int       `json:"count"`
	PriceAudMwh []float64 `json:"price_aud_mwh"`
	PvKw        []float64 `json:"pv_kw"`
	LoadKw      []float64 `json:"load_kw"`
}

// Measured is what the controller saw when it decided.
type Measured struct {
	PriceAudMwh float64 `json:"price_aud_mwh"`
	PvKw        float64 `json:"pv_kw"`
	LoadKw      float64 `json:"load_kw"`
	SocKwh      float64 `json:"soc_kwh"`
}

// StepDecision is the forecast detail for one step.
type StepDecision struct {
	T             string   `json:"t"`
	Action        string   `json:"action"`
	Measured      Measured `json:"measured"`
	EnergyCashAud float64  `json:"energy_cash_aud"`
	Leads         []Lead   `json:"leads"`
	Stories       []Story  `json:"stories"`
}

// RunFile is the on-disk form of a run, backend/runs/<run_id>.json. The
// server reads ticks, summary and steps as raw JSON; the engine writes them
// typed, through this struct.
type RunFile struct {
	RunID      string                  `json:"run_id"`
	WindowName string                  `json:"window_name"`
	Meta       Meta                    `json:"meta"`
	Ticks      []Tick                  `json:"ticks"`
	Summary    Summary                 `json:"summary"`
	Steps      map[string]StepDecision `json:"steps"`
}
