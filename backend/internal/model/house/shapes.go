package house

import "math"

// Deterministic PV and load shapes. No fitted parameters.
//
// Only the pure parts of app/houses/shapes.py are ported. The seeded cloud
// draw and the AR(1) noise use numpy's random generator, so the finished
// traces (pv_unit, load_unit) are exported from Python and read as data.

const (
	LatDeg              = -34.9285
	LonDeg              = 138.6007
	StandardMeridianDeg = 150.0 // civil time of the SA1 index is UTC+10

	// LoadUnitDailyKwh is the daily energy the stored load_unit trace represents.
	LoadUnitDailyKwh = 15.0
)

// loadBlocks is (start hour inclusive, end hour exclusive, share of daily
// energy, hours in the block).
var loadBlocks = [...]struct {
	start, end int
	share      float64
	duration   float64
}{
	{0, 6, 0.10, 6.0},
	{6, 9, 0.15, 3.0},
	{9, 16, 0.20, 7.0},
	{16, 21, 0.40, 5.0},
	{21, 24, 0.15, 3.0},
}

// numpy's radians and degrees multiply by a factor computed once from the
// float64 value of pi, so do the same to match it to the last digit.
const (
	deg2rad = float64(math.Pi) / 180.0
	rad2deg = 180.0 / float64(math.Pi)
)

func radians(deg float64) float64 { return deg * deg2rad }
func degrees(rad float64) float64 { return rad * rad2deg }

// SolarElevationDeg is the sun's elevation for a timestamp on the market
// clock. Negative means the sun is below the horizon.
func SolarElevationDeg(hour, minute, second, dayOfYear int) float64 {
	day := float64(dayOfYear)
	decl := 23.45 * math.Sin(radians(360.0*(284+day)/365.0))
	b := radians(360.0 * (day - 81) / 365.0)
	eotMin := 9.87*math.Sin(2.0*b) - 7.53*math.Cos(b) - 1.5*math.Sin(b)
	offsetMin := 4.0*(LonDeg-StandardMeridianDeg) + eotMin
	clockMin := float64(hour)*60.0 + float64(minute) + float64(second)/60.0
	hourAngle := ((clockMin+offsetMin)/60.0 - 12.0) * 15.0
	lat := radians(LatDeg)
	dec := radians(decl)
	ha := radians(hourAngle)
	sinElev := math.Sin(lat)*math.Sin(dec) + math.Cos(lat)*math.Cos(dec)*math.Cos(ha)
	sinElev = math.Max(-1.0, math.Min(1.0, sinElev))
	return degrees(math.Asin(sinElev))
}

// ClearSkyUnit is kW per kW of AC nameplate under a clear sky, clamped at 0
// overnight.
func ClearSkyUnit(hour, minute, second, dayOfYear int) float64 {
	elev := SolarElevationDeg(hour, minute, second, dayOfYear)
	return math.Max(0.0, math.Sin(radians(elev)))
}

// LoadBaseKw is the flat kW inside each block before noise. A full local day
// sums to dailyLoadKwh.
func LoadBaseKw(hour int, dailyLoadKwh float64) float64 {
	for _, b := range loadBlocks {
		if hour >= b.start && hour < b.end {
			return b.share * dailyLoadKwh / b.duration
		}
	}
	return 0.0
}

// ScaleUnits maps stored units onto this house's PV size and daily load.
func ScaleUnits(pvUnit, loadUnit float64, spec HouseSpec) (pvKw, loadKw float64) {
	return pvUnit * spec.PvKwAc, loadUnit * (spec.DailyLoadKwh / LoadUnitDailyKwh)
}
