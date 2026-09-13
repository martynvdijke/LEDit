package handlers

import (
	"log/slog"
	"math"
	"sync"
	"time"
)

var (
	sunMu        sync.Mutex
	sunLat       *float64
	sunLon       *float64
	sunCachedDay string
	sunRise      int
	sunSet       int
	sunOk        bool
	sunLoggedDay string
)

func SetSunLocation(lat, lon *float64) {
	sunMu.Lock()
	defer sunMu.Unlock()
	sunLat = lat
	sunLon = lon
	// invalidate cache so next SunTimes recomputes
	sunCachedDay = ""
}

func ResetSunState() {
	sunMu.Lock()
	defer sunMu.Unlock()
	sunLat = nil
	sunLon = nil
	sunCachedDay = ""
	sunRise = 0
	sunSet = 0
	sunOk = false
	sunLoggedDay = ""
}

// SunTimes returns sunrise/sunset minutes-of-day for now's calendar day in now.Location().
// If location unset/invalid or polar, returns 07:00/19:00,false and logs once per day.
func SunTimes(now time.Time) (int, int, bool) {
	sunMu.Lock()
	defer sunMu.Unlock()

	dayKey := now.In(now.Location()).Format("2006-01-02")

	if sunCachedDay == dayKey {
		return sunRise, sunSet, sunOk
	}

	// check unset or invalid
	if sunLat == nil || sunLon == nil || *sunLat < -90 || *sunLat > 90 || *sunLon < -180 || *sunLon > 180 {
		if sunLoggedDay != dayKey {
			slog.Warn("sun: location unset or invalid, using fallback window", "day", dayKey)
			sunLoggedDay = dayKey
		}
		sunCachedDay = dayKey
		sunRise = 7 * 60
		sunSet = 19 * 60
		sunOk = false
		return sunRise, sunSet, false
	}

	rise, set, ok := computeSun(*sunLat, *sunLon, now)
	if !ok {
		if sunLoggedDay != dayKey {
			slog.Warn("sun: computation failed (polar), using fallback window", "day", dayKey)
			sunLoggedDay = dayKey
		}
		sunCachedDay = dayKey
		sunRise = 7 * 60
		sunSet = 19 * 60
		sunOk = false
		return sunRise, sunSet, false
	}
	sunCachedDay = dayKey
	sunRise = rise
	sunSet = set
	sunOk = true
	return sunRise, sunSet, true
}

// NOAA approximation: returns minutes-of-day in server-local wall time.
func computeSun(lat, lon float64, now time.Time) (int, int, bool) {
	// Use date in local zone, compute for noon UTC then convert to local via offset
	loc := now.Location()
	localDate := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)
	// day of year
	n := localDate.YearDay()
	// fractional year gamma
	gamma := 2 * math.Pi / 365 * float64(n-1+(12-12)/24.0)
	// eqtime and declination
	eqtime := 229.18 * (0.000075 + 0.001868*math.Cos(gamma) - 0.032077*math.Sin(gamma) - 0.014615*math.Cos(2*gamma) - 0.040849*math.Sin(2*gamma))
	decl := 0.006918 - 0.399912*math.Cos(gamma) + 0.070257*math.Sin(gamma) - 0.006758*math.Cos(2*gamma) + 0.000907*math.Sin(2*gamma) - 0.002697*math.Cos(3*gamma) + 0.00148*math.Sin(3*gamma)
	latRad := lat * math.Pi / 180
	// zenith for official sunrise 90.833 deg
	cosZenith := math.Cos(90.833 * math.Pi / 180)
	cosLat := math.Cos(latRad)
	sinLat := math.Sin(latRad)
	sinDecl := math.Sin(decl)
	cosDecl := math.Cos(decl)
	cosH := (cosZenith - sinLat*sinDecl) / (cosLat * cosDecl)
	if cosH > 1 || cosH < -1 {
		return 0, 0, false
	}
	h := math.Acos(cosH) * 180 / math.Pi // degrees
	// sunrise/sunset in minutes UTC
	sunriseUTC := 720 - 4*(lon+h) - eqtime
	sunsetUTC := 720 - 4*(lon-h) - eqtime
	// Convert UTC minutes to local wall minutes: add timezone offset
	_, offsetSec := localDate.Zone()
	offsetMin := offsetSec / 60
	riseLocal := int(math.Round(sunriseUTC)) + offsetMin
	setLocal := int(math.Round(sunsetUTC)) + offsetMin
	// normalize to 0..1439
	riseLocal = ((riseLocal % 1440) + 1440) % 1440
	setLocal = ((setLocal % 1440) + 1440) % 1440
	return riseLocal, setLocal, true
}
