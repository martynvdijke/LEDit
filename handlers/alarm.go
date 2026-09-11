package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/generalsettings"
	"ledit/ent/wakealarm"
)

// WakeAlarm is the resolution-time view of an alarm definition. It is a plain
// value type so the pure resolver can be tested without a database. The
// AlarmManager converts ent rows into this shape.
type WakeAlarm struct {
	ID                    int
	Name                  string
	Enabled               bool
	Days                  []int
	Start                 string
	End                   string
	WakeSourceType        string
	WakeSourceID          int
	BrightnessEnabled     bool
	BrightnessStart       int
	BrightnessEnd         int
	BrightnessRampSeconds int
}

// alarmScheduleWindow projects an alarm onto the shared schedule matcher.
func alarmScheduleWindow(a *WakeAlarm) ScheduleWindow {
	return ScheduleWindow{Days: a.Days, Start: a.Start, End: a.End}
}

// alarmWindowStart returns the wall-clock instant the current occurrence of a
// started, so overnight windows (end <= start) count from the previous day.
func alarmWindowStart(now time.Time, a *WakeAlarm) time.Time {
	startMin, err := parseHM(a.Start)
	if err != nil {
		return now
	}
	endMin, err2 := parseHM(a.End)
	if err2 != nil {
		return now
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), startMin/60, startMin%60, 0, 0, now.Location())
	if endMin <= startMin {
		nowMin := now.Hour()*60 + now.Minute()
		if nowMin < endMin {
			start = start.AddDate(0, 0, -1)
		}
	}
	return start
}

// OccurrenceKey identifies one firing of an alarm: its ID plus the local date
// the occurrence started on. Keying by the start date (not "today") keeps an
// overnight alarm's dismissal scoped to the night it was dismissed.
func OccurrenceKey(alarmID int, now time.Time, a *WakeAlarm) string {
	start := now
	if a != nil {
		start = alarmWindowStart(now, a)
	}
	return alarmDateKey(alarmID, start)
}

func alarmDateKey(alarmID int, day time.Time) string {
	return itoa(alarmID) + ":" + day.Format("2006-01-02")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ResolveActiveAlarm returns the first enabled alarm whose day/time window
// matches now and which has not been dismissed for the current occurrence.
// Alarms are expected in a stable order (by ID) so resolution is deterministic.
func ResolveActiveAlarm(now time.Time, alarms []WakeAlarm, dismissed map[string]bool) *WakeAlarm {
	for i := range alarms {
		a := &alarms[i]
		if !a.Enabled || len(a.Days) == 0 {
			continue
		}
		if !WindowMatches(now, alarmScheduleWindow(a)) {
			continue
		}
		if dismissed != nil && dismissed[OccurrenceKey(a.ID, now, a)] {
			continue
		}
		return a
	}
	return nil
}

// AlarmBrightnessLevel returns the effective brightness for an active alarm's
// linear ramp, or nil when the alarm has no brightness ramp configured. The
// ramp runs from BrightnessStart to BrightnessEnd over BrightnessRampSeconds
// measured from the occurrence start, then holds BrightnessEnd.
func AlarmBrightnessLevel(now time.Time, a *WakeAlarm) *int {
	if a == nil || !a.BrightnessEnabled {
		return nil
	}
	elapsed := now.Sub(alarmWindowStart(now, a)).Seconds()
	frac := 1.0
	if a.BrightnessRampSeconds > 0 {
		frac = elapsed / float64(a.BrightnessRampSeconds)
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	v := float64(a.BrightnessStart) + (float64(a.BrightnessEnd)-float64(a.BrightnessStart))*frac
	lvl := int(math.Round(v))
	if lvl < 0 {
		lvl = 0
	}
	if lvl > 100 {
		lvl = 100
	}
	return &lvl
}

// AlarmManager is the in-memory source of truth for the currently active wake
// alarm and its resolved wake source. All active state is derived from stored
// definitions and wall time, so a restart simply re-evaluates.
type AlarmManager struct {
	mu        sync.Mutex
	alarms    []WakeAlarm
	active    *WakeAlarm
	source    *sourceWithName
	dismissed map[string]bool
}

// NewAlarmManager creates an empty manager.
func NewAlarmManager() *AlarmManager {
	return &AlarmManager{dismissed: map[string]bool{}}
}

// SetAlarms replaces the cached enabled alarm definitions.
func (m *AlarmManager) SetAlarms(alarms []WakeAlarm) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alarms = alarms
}

// Active returns the active alarm and its resolved source, if any.
func (m *AlarmManager) Active() (*WakeAlarm, *sourceWithName, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil || m.source == nil {
		return nil, nil, false
	}
	return m.active, m.source, true
}

// Source returns the resolved wake source, or nil when no alarm is active.
func (m *AlarmManager) Source() *sourceWithName {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.source
}

// BrightnessLevel returns the active alarm's ramp level, or nil when no alarm
// with a brightness ramp is active.
func (m *AlarmManager) BrightnessLevel(now time.Time) *int {
	m.mu.Lock()
	a := m.active
	m.mu.Unlock()
	return AlarmBrightnessLevel(now, a)
}

// Dismiss suppresses the active alarm for the current occurrence and clears it.
func (m *AlarmManager) Dismiss(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		m.dismissed[OccurrenceKey(m.active.ID, now, m.active)] = true
	}
	m.active = nil
	m.source = nil
}

// Evaluate recomputes the active alarm at now. resolve is invoked only on a
// transition to a new alarm so the source catalog is not hit every tick. It
// returns true when the published wake source changed (activated, cleared, or
// switched).
func (m *AlarmManager) Evaluate(now time.Time, resolve func(*WakeAlarm) (*sourceWithName, bool)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pruneDismissedLocked(now)

	active := ResolveActiveAlarm(now, m.alarms, m.dismissed)
	if active == nil {
		if m.active == nil {
			return false
		}
		m.active = nil
		m.source = nil
		return true
	}
	if m.active != nil && m.active.ID == active.ID {
		return false
	}

	var src *sourceWithName
	if resolve != nil {
		src, _ = resolve(active)
	}
	if src == nil {
		if m.active == nil {
			return false
		}
		m.active = nil
		m.source = nil
		return true
	}
	m.active = active
	m.source = src
	return true
}

// pruneDismissedLocked drops occurrence markers that no longer match the wall
// clock, so the next scheduled firing is not suppressed.
func (m *AlarmManager) pruneDismissedLocked(now time.Time) {
	for k := range m.dismissed {
		keep := false
		for i := range m.alarms {
			a := &m.alarms[i]
			if !a.Enabled || len(a.Days) == 0 {
				continue
			}
			if OccurrenceKey(a.ID, now, a) == k && WindowMatches(now, alarmScheduleWindow(a)) {
				keep = true
				break
			}
		}
		if !keep {
			delete(m.dismissed, k)
		}
	}
}

// globalAlarmManager is the process-wide alarm state read by the feed path and
// written by the event-rule evaluator.
var globalAlarmManager = NewAlarmManager()

// ActiveAlarm returns the currently active alarm and wake source, if any.
func ActiveAlarm() (*WakeAlarm, *sourceWithName, bool) {
	return globalAlarmManager.Active()
}

// AlarmSource returns the resolved wake source for the active alarm, or nil.
func AlarmSource() *sourceWithName {
	return globalAlarmManager.Source()
}

// ActiveAlarmBrightnessLevel returns the active alarm's ramp level, or nil.
func ActiveAlarmBrightnessLevel(now time.Time) *int {
	return globalAlarmManager.BrightnessLevel(now)
}

// DismissActiveAlarm dismisses the active alarm for the current occurrence and
// immediately clears the wake source from every live feed.
func DismissActiveAlarm() {
	globalAlarmManager.Dismiss(time.Now())
	alarmAll(nil)
}

// ---------------------------------------------------------------------------
// Admin CRUD (session-authenticated, mirrors the event-rule form pattern)
// ---------------------------------------------------------------------------

func (s *Server) AdminAlarmList(c *gin.Context) {
	rows, err := s.DB.WakeAlarm.Query().Order(ent.Asc(wakealarm.FieldID)).All(s.Ctx)
	if err != nil {
		rows = []*ent.WakeAlarm{}
	}
	vars := gin.H{
		"alarms":            rows,
		"server_zone":       serverZoneLabel(),
		"alarm_active":      false,
		"alarm_active_name": "",
	}
	if a, src, ok := ActiveAlarm(); ok {
		vars["alarm_active"] = true
		vars["alarm_active_name"] = a.Name
		if src != nil {
			vars["alarm_active_source"] = src.Name
		}
	}
	s.renderPage(c, http.StatusOK, "alarms.html", vars)
}

func alarmFormVars(name string, enabled bool, days []int, start, end, srcType string, srcID int, bEnabled bool, bStart, bEnd, bRamp int) gin.H {
	dm := map[int]bool{}
	for _, d := range days {
		dm[d] = true
	}
	return gin.H{
		"fName":     name,
		"fEnabled":  enabled,
		"fDays":     dm,
		"fStart":    start,
		"fEnd":      end,
		"fSrcType":  srcType,
		"fSrcID":    strconv.Itoa(srcID),
		"fBEnabled": bEnabled,
		"fBStart":   bStart,
		"fBEnd":     bEnd,
		"fBRamp":    bRamp,
	}
}

func (s *Server) AdminAlarmNew(c *gin.Context) {
	opts := s.bindingOptions(c)
	vars := alarmFormVars("", true, nil, "06:30", "07:00", "", 0, false, 0, 100, 600)
	vars["options"] = opts
	vars["options_json"] = bindingOptionsJSON(opts)
	vars["server_zone"] = serverZoneLabel()
	s.renderPage(c, http.StatusOK, "alarm_form.html", vars)
}

func alarmFormFromPost(c *gin.Context) (name string, enabled bool, days []int, start, end, srcType string, srcID int, bEnabled bool, bStart, bEnd, bRamp int) {
	name = c.PostForm("name")
	enabled = c.PostForm("enabled") == "on"
	for _, d := range c.PostFormArray("days") {
		if v, err := strconv.Atoi(d); err == nil {
			days = append(days, v)
		}
	}
	start = c.PostForm("start")
	if start == "" {
		start = "00:00"
	}
	end = c.PostForm("end")
	if end == "" {
		end = "00:00"
	}
	srcType = c.PostForm("wake_source_type")
	srcID, _ = strconv.Atoi(c.PostForm("wake_source_id"))
	bEnabled = c.PostForm("brightness_enabled") == "on"
	bStart = atoiOr(c.PostForm("brightness_start"), 0)
	bEnd = atoiOr(c.PostForm("brightness_end"), 100)
	bRamp = atoiOr(c.PostForm("brightness_ramp_seconds"), 600)
	return
}

func validateAlarm(s *Server, c *gin.Context, name string, days []int, start, end, srcType string, srcID, bStart, bEnd, bRamp int) string {
	if strings.TrimSpace(name) == "" {
		return "name is required"
	}
	if len(days) == 0 {
		return "select at least one day"
	}
	seen := map[int]bool{}
	for _, d := range days {
		if d < 0 || d > 6 {
			return fmt.Sprintf("day %d out of range 0-6", d)
		}
		if seen[d] {
			return fmt.Sprintf("duplicate day %d", d)
		}
		seen[d] = true
	}
	if _, err := parseHM(start); err != nil {
		return "invalid start time (HH:MM)"
	}
	if _, err := parseHM(end); err != nil {
		return "invalid end time (HH:MM)"
	}
	if start == end {
		return "start and end must differ"
	}
	opts := s.bindingOptions(c)
	found := false
	if list, ok := opts[srcType]; ok {
		for _, o := range list {
			if o.ID == srcID {
				found = true
				break
			}
		}
	}
	if !found {
		return fmt.Sprintf("wake source %s:%d not found", srcType, srcID)
	}
	if bStart < 0 || bStart > 100 || bEnd < 0 || bEnd > 100 {
		return "brightness must be between 0 and 100"
	}
	if bRamp < 0 || bRamp > 3600 {
		return "brightness_ramp_seconds must be between 0 and 3600"
	}
	return ""
}

func (s *Server) AdminAlarmCreate(c *gin.Context) {
	name, enabled, days, start, end, srcType, srcID, bEnabled, bStart, bEnd, bRamp := alarmFormFromPost(c)
	if msg := validateAlarm(s, c, name, days, start, end, srcType, srcID, bStart, bEnd, bRamp); msg != "" {
		SetFlash(c, "danger", msg)
		opts := s.bindingOptions(c)
		vars := alarmFormVars(name, enabled, days, start, end, srcType, srcID, bEnabled, bStart, bEnd, bRamp)
		vars["error"] = msg
		vars["options"] = opts
		vars["options_json"] = bindingOptionsJSON(opts)
		vars["server_zone"] = serverZoneLabel()
		// Spec: validation failures reject with 400 while still showing the error.
		s.renderPage(c, http.StatusBadRequest, "alarm_form.html", vars)
		return
	}
	daysJSON, _ := json.Marshal(days)
	obj, err := s.DB.WakeAlarm.Create().
		SetName(name).SetEnabled(enabled).SetDays(string(daysJSON)).
		SetStart(start).SetEnd(end).
		SetWakeSourceType(srcType).SetWakeSourceID(srcID).
		SetBrightnessEnabled(bEnabled).SetBrightnessStart(bStart).SetBrightnessEnd(bEnd).
		SetBrightnessRampSeconds(bRamp).Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		opts := s.bindingOptions(c)
		vars := alarmFormVars(name, enabled, days, start, end, srcType, srcID, bEnabled, bStart, bEnd, bRamp)
		vars["options"] = opts
		vars["options_json"] = bindingOptionsJSON(opts)
		vars["server_zone"] = serverZoneLabel()
		s.renderPage(c, http.StatusOK, "alarm_form.html", vars)
		return
	}
	if gs, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil && gs != nil {
		s.DB.GeneralSettings.UpdateOne(gs).AddWakealarms(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Wake alarm created")
	c.Redirect(http.StatusFound, "/admin/alarms")
}

func (s *Server) AdminAlarmEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.WakeAlarm.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Wake alarm not found")
		c.Redirect(http.StatusFound, "/admin/alarms")
		return
	}
	opts := s.bindingOptions(c)
	vars := alarmFormVars(obj.Name, obj.Enabled, alarmDaysFromString(obj.Days), obj.Start, obj.End, obj.WakeSourceType, obj.WakeSourceID, obj.BrightnessEnabled, obj.BrightnessStart, obj.BrightnessEnd, obj.BrightnessRampSeconds)
	vars["obj"] = obj
	vars["edit"] = true
	vars["id"] = id
	vars["options"] = opts
	vars["options_json"] = bindingOptionsJSON(opts)
	vars["server_zone"] = serverZoneLabel()
	s.renderPage(c, http.StatusOK, "alarm_form.html", vars)
}

func (s *Server) AdminAlarmUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name, enabled, days, start, end, srcType, srcID, bEnabled, bStart, bEnd, bRamp := alarmFormFromPost(c)
	if msg := validateAlarm(s, c, name, days, start, end, srcType, srcID, bStart, bEnd, bRamp); msg != "" {
		SetFlash(c, "danger", msg)
		opts := s.bindingOptions(c)
		vars := alarmFormVars(name, enabled, days, start, end, srcType, srcID, bEnabled, bStart, bEnd, bRamp)
		vars["error"] = msg
		vars["edit"] = true
		vars["id"] = id
		vars["options"] = opts
		vars["options_json"] = bindingOptionsJSON(opts)
		vars["server_zone"] = serverZoneLabel()
		s.renderPage(c, http.StatusBadRequest, "alarm_form.html", vars)
		return
	}
	daysJSON, _ := json.Marshal(days)
	if err := s.DB.WakeAlarm.UpdateOneID(id).
		SetName(name).SetEnabled(enabled).SetDays(string(daysJSON)).
		SetStart(start).SetEnd(end).
		SetWakeSourceType(srcType).SetWakeSourceID(srcID).
		SetBrightnessEnabled(bEnabled).SetBrightnessStart(bStart).SetBrightnessEnd(bEnd).
		SetBrightnessRampSeconds(bRamp).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/alarms")
		return
	}
	SetFlash(c, "success", "Wake alarm updated")
	c.Redirect(http.StatusFound, "/admin/alarms")
}

func (s *Server) AdminAlarmDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.WakeAlarm.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/alarms")
		return
	}
	SetFlash(c, "success", "Wake alarm deleted")
	c.Redirect(http.StatusFound, "/admin/alarms")
}

func alarmDaysFromString(s string) []int {
	days, _ := parseAlarmDays(s)
	return days
}
