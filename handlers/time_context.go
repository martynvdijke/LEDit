package handlers

import (
	"encoding/json"
	"strings"

	"ledit/ent"
)

func RefreshTimeContext(settings *ent.GeneralSettings) {
	if settings == nil {
		SetSunLocation(nil, nil)
		SetHolidays(nil, nil)
		return
	}
	SetSunLocation(settings.Latitude, settings.Longitude)
	var staticDates []string
	if strings.TrimSpace(settings.Holidays) != "" {
		_ = json.Unmarshal([]byte(settings.Holidays), &staticDates)
	}
	var icsDates []string
	if strings.TrimSpace(settings.HolidayIcsDates) != "" {
		_ = json.Unmarshal([]byte(settings.HolidayIcsDates), &icsDates)
	}
	SetHolidays(staticDates, icsDates)
}
