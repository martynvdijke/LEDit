package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent/countdown"
	"ledit/ent/generalsettings"
)

// countdownTimeLayout is the value of <input type="datetime-local">.
const countdownTimeLayout = "2006-01-02T15:04"

// countdownGranularities, countdownDirections, and countdownCompletions mirror
// the ent schema enum values so handler validation cannot drift from it.
var (
	countdownGranularities = []string{
		string(countdown.GranularitySeconds),
		string(countdown.GranularityMinutes),
		string(countdown.GranularityHours),
		string(countdown.GranularityDays),
	}
	countdownDirections = []string{
		string(countdown.DirectionDown),
		string(countdown.DirectionUp),
	}
	countdownCompletions = []string{
		string(countdown.CompletionNow),
		string(countdown.CompletionMessageText),
		string(countdown.CompletionHide),
	}
)

// parseCountdownTime parses a wall-clock datetime-local value in the given IANA
// timezone (empty uses the server-local zone) and returns the absolute instant.
func parseCountdownTime(layout, value, tz string) (time.Time, error) {
	loc := time.Local
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return time.Time{}, err
		}
		loc = l
	}
	return time.ParseInLocation(layout, value, loc)
}

// countdownFormData holds the validated countdown form values.
type countdownFormData struct {
	name        string
	label       string
	granularity string
	direction   string
	completion  string
	message     string
	timezone    string
	enabled     bool
	target      time.Time
}

// parseCountdownForm reads and validates the shared countdown form fields.
func parseCountdownForm(c *gin.Context) (countdownFormData, *Validator) {
	d := countdownFormData{
		name:        c.PostForm("name"),
		label:       c.PostForm("label"),
		granularity: c.DefaultPostForm("granularity", string(countdown.GranularitySeconds)),
		direction:   c.DefaultPostForm("direction", string(countdown.DirectionDown)),
		completion:  c.DefaultPostForm("completion", string(countdown.CompletionNow)),
		message:     c.PostForm("completion_message"),
		timezone:    strings.TrimSpace(c.PostForm("timezone")),
		enabled:     c.PostForm("enabled") == "on",
	}

	v := NewValidator().Required("Name", d.name).
		MaxLen("Label", d.label, 64).
		OneOf("Granularity", d.granularity, countdownGranularities...).
		OneOf("Direction", d.direction, countdownDirections...).
		OneOf("Completion", d.completion, countdownCompletions...)
	if d.completion == string(countdown.CompletionMessageText) {
		v.MaxLen("Completion message", d.message, 32)
		if strings.TrimSpace(d.message) == "" {
			v.Errors = append(v.Errors, ValidationError{Field: "Completion message", Message: "is required when completion is message"})
		}
	}
	if d.timezone != "" {
		if _, err := time.LoadLocation(d.timezone); err != nil {
			v.Errors = append(v.Errors, ValidationError{Field: "Timezone", Message: "must be a valid IANA location"})
		}
	}
	target, err := parseCountdownTime(countdownTimeLayout, c.PostForm("target_time"), d.timezone)
	if err != nil {
		v.Errors = append(v.Errors, ValidationError{Field: "Target time", Message: "is required"})
	}
	d.target = target
	return d, v
}

func (s *Server) AdminCountdownNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "countdown_form.html", gin.H{})
}

func (s *Server) AdminCountdownCreate(c *gin.Context) {
	d, v := parseCountdownForm(c)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/countdowns/new")
		return
	}

	obj := s.DB.Countdown.Create().
		SetName(d.name).SetTargetTime(d.target).SetLabel(d.label).SetEnabled(d.enabled).
		SetGranularity(countdown.Granularity(d.granularity)).
		SetDirection(countdown.Direction(d.direction)).
		SetCompletion(countdown.Completion(d.completion)).
		SetCompletionMessage(d.message).
		SetTimezone(d.timezone).
		SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddCountdowns(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Countdown created")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminCountdownEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Countdown.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Countdown not found")
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	targetLocal := obj.TargetTime
	if obj.Timezone != "" {
		if loc, err := time.LoadLocation(obj.Timezone); err == nil {
			targetLocal = obj.TargetTime.In(loc)
		}
	}
	s.renderPage(c, http.StatusOK, "countdown_form.html", gin.H{
		"obj":          obj,
		"edit":         true,
		"target_local": targetLocal.Format(countdownTimeLayout),
	})
}

func (s *Server) AdminCountdownUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	d, v := parseCountdownForm(c)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/countdowns/"+strconv.Itoa(id)+"/edit")
		return
	}

	s.DB.Countdown.UpdateOneID(id).
		SetName(d.name).SetTargetTime(d.target).SetLabel(d.label).SetEnabled(d.enabled).
		SetGranularity(countdown.Granularity(d.granularity)).
		SetDirection(countdown.Direction(d.direction)).
		SetCompletion(countdown.Completion(d.completion)).
		SetCompletionMessage(d.message).
		SetTimezone(d.timezone).
		Exec(s.Ctx)
	SetFlash(c, "success", "Countdown updated")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminCountdownDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Countdown.DeleteOneID(id).Exec(s.Ctx)
	SetFlash(c, "success", "Countdown deleted")
	c.Redirect(http.StatusFound, "/admin/")
}
