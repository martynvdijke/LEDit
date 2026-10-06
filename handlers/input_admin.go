package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ledit/ent"
	"ledit/ent/inputbinding"
)

// inputBindingInput is the admin API payload for creating or updating an input
// binding. DeviceID nil means the binding applies to every device.
type inputBindingInput struct {
	DeviceID *int   `json:"device_id"`
	Source   string `json:"source"`
	Event    string `json:"event"`
	Match    string `json:"match"`
	Action   string `json:"action"`
	Enabled  *bool  `json:"enabled"`
	Order    *int   `json:"order"`
}

// validInputEventName reports whether the event belongs to the closed input
// event vocabulary. Empty is allowed on bindings and matches any event.
func validInputEventName(event string) bool {
	switch event {
	case InputEventPress, InputEventRotate, InputEventTap, InputEventPresence, InputEventLux:
		return true
	}
	return false
}

// validateInputBindingPayload applies structural validation (source/event
// vocabulary, action grammar) plus DB-backed target validation.
func (s *Server) validateInputBindingPayload(in inputBindingInput) string {
	source := strings.TrimSpace(in.Source)
	if source != "" && !validInputSource(source) {
		return "unknown input source"
	}
	if event := strings.TrimSpace(in.Event); event != "" && !validInputEventName(event) {
		return "unknown input event"
	}
	a, err := ParseInputAction(in.Action)
	if err != nil {
		return err.Error()
	}
	return s.ValidateInputActionTargets(s.Ctx, a)
}

// AdminInputBindings renders the input bindings admin page.
func (s *Server) AdminInputBindings(c *gin.Context) {
	rows, err := s.DB.InputBinding.Query().Order(ent.Asc(inputbinding.FieldID)).All(s.Ctx)
	if err != nil {
		rows = []*ent.InputBinding{}
	}
	s.renderPage(c, http.StatusOK, "input_bindings.html", gin.H{
		"bindings": rows,
		"active":   "input-bindings",
	})
}

// APIInputBindingList returns every input binding ordered by id.
func (s *Server) APIInputBindingList(c *gin.Context) {
	rows, err := s.DB.InputBinding.Query().Order(ent.Asc(inputbinding.FieldID)).All(s.Ctx)
	if err != nil {
		c.JSON(http.StatusOK, []*ent.InputBinding{})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// APIInputBindingCreate stores a new input binding after validation.
func (s *Server) APIInputBindingCreate(c *gin.Context) {
	var in inputBindingInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if msg := s.validateInputBindingPayload(in); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	order := 0
	if in.Order != nil {
		order = *in.Order
	}
	obj, err := s.DB.InputBinding.Create().
		SetNillableDeviceID(in.DeviceID).
		SetSource(strings.TrimSpace(in.Source)).
		SetEvent(strings.TrimSpace(in.Event)).
		SetMatch(strings.TrimSpace(in.Match)).
		SetAction(in.Action).
		SetEnabled(enabled).
		SetOrder(order).
		Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, obj)
}

// APIInputBindingUpdate updates an existing input binding.
func (s *Server) APIInputBindingUpdate(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := s.DB.InputBinding.Get(s.Ctx, id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var in inputBindingInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if msg := s.validateInputBindingPayload(in); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	upd := s.DB.InputBinding.UpdateOneID(id).
		SetNillableDeviceID(in.DeviceID).
		SetSource(strings.TrimSpace(in.Source)).
		SetEvent(strings.TrimSpace(in.Event)).
		SetMatch(strings.TrimSpace(in.Match)).
		SetAction(in.Action)
	if in.Enabled != nil {
		upd = upd.SetEnabled(*in.Enabled)
	}
	if in.Order != nil {
		upd = upd.SetOrder(*in.Order)
	}
	obj, err := upd.Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, obj)
}

// APIInputBindingDelete removes an input binding.
func (s *Server) APIInputBindingDelete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := s.DB.InputBinding.DeleteOneID(id).Exec(s.Ctx); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// APIInputBindingToggle flips a binding's enabled flag.
func (s *Server) APIInputBindingToggle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	row, err := s.DB.InputBinding.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	obj, err := s.DB.InputBinding.UpdateOneID(id).SetEnabled(!row.Enabled).Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, obj)
}
