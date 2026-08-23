package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/displayrule"
	"ledit/ent/generalsettings"
)

// ---------------------------------------------------------------------------
// Controller registry
// ---------------------------------------------------------------------------

var (
	controllersMu sync.Mutex
	controllers   = map[*FeedController]struct{}{}
)

func joinController(fc *FeedController) {
	controllersMu.Lock()
	defer controllersMu.Unlock()
	controllers[fc] = struct{}{}
}

func leaveController(fc *FeedController) {
	controllersMu.Lock()
	defer controllersMu.Unlock()
	delete(controllers, fc)
}

func pinAll(key, by string) {
	controllersMu.Lock()
	defer controllersMu.Unlock()
	for fc := range controllers {
		fc.Pin(key, by)
	}
}

func unpinAll() {
	controllersMu.Lock()
	defer controllersMu.Unlock()
	for fc := range controllers {
		fc.Unpin()
	}
}

// ---------------------------------------------------------------------------
// Engine lifecycle
// ---------------------------------------------------------------------------

var (
	engineMu     sync.Mutex
	engineCancel context.CancelFunc
	engineClient *ent.Client

	// RuleTargetResolver is overridable in tests.
	RuleTargetResolver func(sourceType string, sourceID int) (datasource.Datasource, bool)

	nonCapableLogged = map[string]bool{}
	nonCapableMu     sync.Mutex
)

func StartEventRuleEngine(client *ent.Client) {
	engineMu.Lock()
	defer engineMu.Unlock()
	if engineCancel != nil {
		return
	}
	engineClient = client
	ctx, cancel := context.WithCancel(context.Background())
	engineCancel = cancel
	// default resolver: builtin trio
	if RuleTargetResolver == nil {
		RuleTargetResolver = func(sourceType string, sourceID int) (datasource.Datasource, bool) {
			switch sourceType {
			case "systemstats":
				return &datasource.SystemStatsDS{}, true
			case "analog-clock":
				return &datasource.AnalogClockDS{}, true
			case "matrix-rain":
				return &datasource.MatrixRainDS{}, true
			default:
				return nil, false
			}
		}
	}
	go runEvaluator(ctx, client)
	slog.Info("event rule engine started")
}

func StopEventRuleEngine() {
	engineMu.Lock()
	defer engineMu.Unlock()
	if engineCancel != nil {
		engineCancel()
		engineCancel = nil
		engineClient = nil
		slog.Info("event rule engine stopped")
	}
}

// Helpers for tests
func ResetNonCapableLogged() {
	nonCapableMu.Lock()
	defer nonCapableMu.Unlock()
	nonCapableLogged = map[string]bool{}
}

type ruleState struct {
	rule          *ent.DisplayRule
	nextCheck     time.Time
	cooldownUntil time.Time
	pinned        bool
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	// ±10%
	delta := float64(d) * 0.1
	j := (rand.Float64()*2 - 1) * delta
	return d + time.Duration(j)
}

func resolveTarget(sourceType string, sourceID int, client *ent.Client) (datasource.Datasource, bool) {
	// Try custom resolver first (test seam)
	if RuleTargetResolver != nil {
		if ds, ok := RuleTargetResolver(sourceType, sourceID); ok {
			return ds, true
		}
	}
	// Try DB-backed catalog via GeneralSettings edges
	if client != nil {
		ctx := context.Background()
		gs, err := client.GeneralSettings.Query().Where(generalsettings.ID(1)).
			WithGenericApis().WithHomeAssistant().WithWeather().WithSonarr().WithRadarr().WithF1().WithUntappd().WithCrypto().WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().WithGoogleCalendars().WithNewsFeeds().WithMatrixLayouts().WithCountdowns().WithAiDigests().
			Only(ctx)
		if err == nil && gs != nil {
			aiCfg := datasource.AIConfig{}
			if ai, err := client.AISettings.Query().Only(ctx); err == nil && ai != nil {
				aiCfg = datasource.AIConfig{Provider: ai.Provider, Endpoint: ai.Endpoint, APIKey: ai.APIKey, Model: ai.Model}
			}
			idx := buildSourceIndex(gs, aiCfg)
			if ds, _, err := idx.Resolve(sourceType, sourceID); err == nil {
				return ds, true
			}
		}
		// Also check builtin trio directly if resolver didn't cover
		switch sourceType {
		case "systemstats":
			return &datasource.SystemStatsDS{}, true
		case "analog-clock":
			return &datasource.AnalogClockDS{}, true
		case "matrix-rain":
			return &datasource.MatrixRainDS{}, true
		}
	}
	return nil, false
}

func runEvaluator(ctx context.Context, client *ent.Client) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("event rule evaluator panic recovered", "panic", r)
			// restart after short delay if context not cancelled
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				go runEvaluator(ctx, client)
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	reloadTicker := time.NewTicker(30 * time.Second)
	defer reloadTicker.Stop()

	states := map[int]*ruleState{}

	loadRules := func() {
		rules, err := client.DisplayRule.Query().Where(displayrule.EnabledEQ(true)).All(ctx)
		if err != nil {
			slog.Error("event evaluator: failed to load rules", "error", err)
			return
		}
		seen := map[int]bool{}
		for _, r := range rules {
			seen[r.ID] = true
			if _, ok := states[r.ID]; !ok {
				states[r.ID] = &ruleState{rule: r, nextCheck: time.Now().Add(jitter(time.Duration(r.CheckIntervalSeconds) * time.Second))}
			} else {
				states[r.ID].rule = r
			}
		}
		for id := range states {
			if !seen[id] {
				delete(states, id)
			}
		}
	}
	loadRules()

	for {
		select {
		case <-ctx.Done():
			return
		case <-reloadTicker.C:
			loadRules()
		case <-ticker.C:
			now := time.Now()
			for _, rs := range states {
				if now.Before(rs.nextCheck) {
					continue
				}
				// schedule next check
				interval := time.Duration(rs.rule.CheckIntervalSeconds) * time.Second
				if interval < 5*time.Second {
					interval = 5 * time.Second
				}
				rs.nextCheck = now.Add(jitter(interval))

				// cooldown suppression: if cooldown active, skip
				if now.Before(rs.cooldownUntil) {
					continue
				}

				ds, ok := resolveTarget(rs.rule.SourceType, rs.rule.SourceID, client)
				if !ok {
					cacheKey := fmt.Sprintf("%s:%d", rs.rule.SourceType, rs.rule.SourceID)
					nonCapableMu.Lock()
					already := nonCapableLogged[cacheKey]
					if !already {
						nonCapableLogged[cacheKey] = true
						slog.Warn("event rule target not resolvable, skipping", "rule", rs.rule.Name, "cacheKey", cacheKey)
					}
					nonCapableMu.Unlock()
					continue
				}
				sp, ok := ds.(datasource.StateProvider)
				if !ok {
					cacheKey := fmt.Sprintf("%s:%d", rs.rule.SourceType, rs.rule.SourceID)
					nonCapableMu.Lock()
					already := nonCapableLogged[cacheKey]
					if !already {
						nonCapableLogged[cacheKey] = true
						slog.Warn("event rule target not StateProvider, skipping", "rule", rs.rule.Name, "cacheKey", cacheKey)
					}
					nonCapableMu.Unlock()
					continue
				}
				cond, err := datasource.ParseCondition(rs.rule.Condition)
				if err != nil {
					slog.Warn("event rule condition parse failed", "rule", rs.rule.Name, "error", err)
					continue
				}
				fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				state, err := sp.CurrentState(fetchCtx)
				cancel()
				if err != nil {
					slog.Warn("event rule state fetch failed", "rule", rs.rule.Name, "error", err)
					continue
				}
				result := datasource.Evaluate(state, cond)
				cacheKey := fmt.Sprintf("%s:%d", rs.rule.SourceType, rs.rule.SourceID)
				if result {
					if !rs.pinned {
						// enforce min-hold: cooldown already handled via cooldownUntil check before fetch, but also need min-hold after fire?
						// D6: cooldown acts as min-hold after fire. So after pin, set cooldownUntil? Actually min-hold means don't unpin until cooldown seconds after fire.
						// We'll store pinned time and suppress unpin until cooldown elapsed. For pin direction, cooldown suppresses re-fire after release.
						// So pin case: set pinned=true and broadcast
						pinAll(cacheKey, rs.rule.Name)
						rs.pinned = true
						if rs.rule.CooldownSeconds > 0 {
							rs.cooldownUntil = now.Add(time.Duration(rs.rule.CooldownSeconds) * time.Second)
						}
					} else {
						// already pinned, extend cooldown? Keep min-hold: don't unpin until cooldown after fire. Already pinned, keep pinned.
						// cooldown for flicker tolerance: if condition flickers false then true within cooldown, still pinned so no change
						if rs.rule.CooldownSeconds > 0 {
							// reset cooldown hold
							rs.cooldownUntil = now.Add(time.Duration(rs.rule.CooldownSeconds) * time.Second)
						}
					}
				} else {
					if rs.pinned {
						// check min-hold: if cooldownUntil still in future, suppress unpin
						if now.Before(rs.cooldownUntil) {
							continue
						}
						unpinAll()
						rs.pinned = false
						if rs.rule.CooldownSeconds > 0 {
							rs.cooldownUntil = now.Add(time.Duration(rs.rule.CooldownSeconds) * time.Second)
						}
					}
				}
			}
		}
	}
}

// EvaluateRulesOnce is exported for tests: runs one evaluation cycle synchronously.
func EvaluateRulesOnce(client *ent.Client, states map[int]*ruleState) {
	if states == nil {
		return
	}
	now := time.Now()
	for _, rs := range states {
		if now.Before(rs.cooldownUntil) {
			continue
		}
		ds, ok := resolveTarget(rs.rule.SourceType, rs.rule.SourceID, client)
		if !ok {
			cacheKey := fmt.Sprintf("%s:%d", rs.rule.SourceType, rs.rule.SourceID)
			nonCapableMu.Lock()
			if !nonCapableLogged[cacheKey] {
				nonCapableLogged[cacheKey] = true
				slog.Warn("event rule target not resolvable, skipping", "rule", rs.rule.Name, "cacheKey", cacheKey)
			}
			nonCapableMu.Unlock()
			continue
		}
		sp, ok := ds.(datasource.StateProvider)
		if !ok {
			cacheKey := fmt.Sprintf("%s:%d", rs.rule.SourceType, rs.rule.SourceID)
			nonCapableMu.Lock()
			if !nonCapableLogged[cacheKey] {
				nonCapableLogged[cacheKey] = true
				slog.Warn("event rule target not StateProvider, skipping", "rule", rs.rule.Name, "cacheKey", cacheKey)
			}
			nonCapableMu.Unlock()
			continue
		}
		cond, err := datasource.ParseCondition(rs.rule.Condition)
		if err != nil {
			continue
		}
		fetchCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, err := sp.CurrentState(fetchCtx)
		cancel()
		if err != nil {
			continue
		}
		result := datasource.Evaluate(state, cond)
		cacheKey := fmt.Sprintf("%s:%d", rs.rule.SourceType, rs.rule.SourceID)
		if result {
			if !rs.pinned {
				pinAll(cacheKey, rs.rule.Name)
				rs.pinned = true
				if rs.rule.CooldownSeconds > 0 {
					rs.cooldownUntil = now.Add(time.Duration(rs.rule.CooldownSeconds) * time.Second)
				}
			}
		} else {
			if rs.pinned {
				if now.Before(rs.cooldownUntil) {
					continue
				}
				unpinAll()
				rs.pinned = false
				if rs.rule.CooldownSeconds > 0 {
					rs.cooldownUntil = now.Add(time.Duration(rs.rule.CooldownSeconds) * time.Second)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// CRUD handlers
// ---------------------------------------------------------------------------

func (s *Server) AdminEventRuleList(c *gin.Context) {
	rows, err := s.DB.DisplayRule.Query().Order(ent.Asc(displayrule.FieldID)).All(s.Ctx)
	if err != nil {
		rows = []*ent.DisplayRule{}
	}
	s.renderPage(c, http.StatusOK, "eventrules.html", gin.H{"eventrules": rows})
}

// eventRuleFormVars supplies flat prepopulation strings for
// eventrule_form.html so ent rows and error-replay maps render identically
// (templates never branch on the obj type).
func eventRuleFormVars(name, srcType, srcID, condition, interval, cooldown string, enabled bool) gin.H {
	e := ""
	if enabled {
		e = "on"
	}
	return gin.H{
		"fName":      name,
		"fEnabled":   e,
		"fSrcType":   srcType,
		"fSrcID":     srcID,
		"fCondition": condition,
		"fInterval":  interval,
		"fCooldown":  cooldown,
	}
}

func (s *Server) AdminEventRuleNew(c *gin.Context) {
	opts := s.bindingOptions(c)
	vars := eventRuleFormVars("", "", "", "{}", "30", "0", true)
	vars["options"] = opts
	vars["options_json"] = bindingOptionsJSON(opts)
	s.renderPage(c, http.StatusOK, "eventrule_form.html", vars)
}

func validateEventRule(s *Server, c *gin.Context, name, sourceType string, sourceID int, condition string, checkInterval, cooldown int) string {
	if name == "" {
		return "name is required"
	}
	// validate target exists in catalog
	opts := s.bindingOptions(c)
	found := false
	if list, ok := opts[sourceType]; ok {
		for _, o := range list {
			if o.ID == sourceID {
				found = true
				break
			}
		}
	}
	if !found {
		return fmt.Sprintf("target %s:%d not found", sourceType, sourceID)
	}
	if _, err := datasource.ParseCondition(condition); err != nil {
		return "invalid condition: " + err.Error()
	}
	if checkInterval < 5 {
		return "check_interval_seconds must be >= 5"
	}
	if cooldown < 0 {
		return "cooldown_seconds must be >= 0"
	}
	return ""
}

func (s *Server) AdminEventRuleCreate(c *gin.Context) {
	name := c.PostForm("name")
	sourceType := c.PostForm("source_type")
	sourceID, _ := strconv.Atoi(c.PostForm("source_id"))
	condition := c.PostForm("condition")
	if condition == "" {
		condition = "{}"
	}
	checkInterval, _ := strconv.Atoi(c.PostForm("check_interval_seconds"))
	if c.PostForm("check_interval_seconds") == "" {
		checkInterval = 30
	}
	cooldown, _ := strconv.Atoi(c.PostForm("cooldown_seconds"))
	enabled := c.PostForm("enabled") == "on"

	if msg := validateEventRule(s, c, name, sourceType, sourceID, condition, checkInterval, cooldown); msg != "" {
		SetFlash(c, "danger", msg)
		opts := s.bindingOptions(c)
		vars := eventRuleFormVars(name, sourceType, strconv.Itoa(sourceID), condition, strconv.Itoa(checkInterval), strconv.Itoa(cooldown), enabled)
		vars["obj"] = map[string]string{"name": name, "source_type": sourceType, "source_id": strconv.Itoa(sourceID), "condition": condition, "check_interval_seconds": strconv.Itoa(checkInterval), "cooldown_seconds": strconv.Itoa(cooldown)}
		vars["error"] = msg
		vars["options"] = opts
		vars["options_json"] = bindingOptionsJSON(opts)
		s.renderPage(c, http.StatusOK, "eventrule_form.html", vars)
		return
	}
	obj, err := s.DB.DisplayRule.Create().SetName(name).SetEnabled(enabled).SetSourceType(sourceType).SetSourceID(sourceID).SetCondition(condition).SetCheckIntervalSeconds(checkInterval).SetCooldownSeconds(cooldown).Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		opts := s.bindingOptions(c)
		vars := eventRuleFormVars(name, sourceType, strconv.Itoa(sourceID), condition, strconv.Itoa(checkInterval), strconv.Itoa(cooldown), enabled)
		vars["options"] = opts
		vars["options_json"] = bindingOptionsJSON(opts)
		s.renderPage(c, http.StatusOK, "eventrule_form.html", vars)
		return
	}
	if gs, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil && gs != nil {
		s.DB.GeneralSettings.UpdateOne(gs).AddDisplayrules(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Event rule created")
	c.Redirect(http.StatusFound, "/admin/eventrules")
}

func (s *Server) AdminEventRuleEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.DisplayRule.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Event rule not found")
		c.Redirect(http.StatusFound, "/admin/eventrules")
		return
	}
	opts := s.bindingOptions(c)
	vars := eventRuleFormVars(obj.Name, obj.SourceType, strconv.Itoa(obj.SourceID), obj.Condition, strconv.Itoa(obj.CheckIntervalSeconds), strconv.Itoa(obj.CooldownSeconds), obj.Enabled)
	vars["obj"] = obj
	vars["edit"] = true
	vars["id"] = id
	vars["options"] = opts
	vars["options_json"] = bindingOptionsJSON(opts)
	s.renderPage(c, http.StatusOK, "eventrule_form.html", vars)
}

func (s *Server) AdminEventRuleUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := c.PostForm("name")
	sourceType := c.PostForm("source_type")
	sourceID, _ := strconv.Atoi(c.PostForm("source_id"))
	condition := c.PostForm("condition")
	if condition == "" {
		condition = "{}"
	}
	checkInterval, _ := strconv.Atoi(c.PostForm("check_interval_seconds"))
	if c.PostForm("check_interval_seconds") == "" {
		checkInterval = 30
	}
	cooldown, _ := strconv.Atoi(c.PostForm("cooldown_seconds"))
	enabled := c.PostForm("enabled") == "on"

	if msg := validateEventRule(s, c, name, sourceType, sourceID, condition, checkInterval, cooldown); msg != "" {
		SetFlash(c, "danger", msg)
		opts := s.bindingOptions(c)
		vars := eventRuleFormVars(name, sourceType, strconv.Itoa(sourceID), condition, strconv.Itoa(checkInterval), strconv.Itoa(cooldown), enabled)
		vars["obj"] = map[string]string{"name": name, "source_type": sourceType, "source_id": strconv.Itoa(sourceID), "condition": condition, "check_interval_seconds": strconv.Itoa(checkInterval), "cooldown_seconds": strconv.Itoa(cooldown)}
		vars["edit"] = true
		vars["id"] = id
		vars["error"] = msg
		vars["options"] = opts
		vars["options_json"] = bindingOptionsJSON(opts)
		s.renderPage(c, http.StatusOK, "eventrule_form.html", vars)
		return
	}
	if err := s.DB.DisplayRule.UpdateOneID(id).SetName(name).SetEnabled(enabled).SetSourceType(sourceType).SetSourceID(sourceID).SetCondition(condition).SetCheckIntervalSeconds(checkInterval).SetCooldownSeconds(cooldown).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		opts := s.bindingOptions(c)
		vars := eventRuleFormVars(name, sourceType, strconv.Itoa(sourceID), condition, strconv.Itoa(checkInterval), strconv.Itoa(cooldown), enabled)
		vars["edit"] = true
		vars["id"] = id
		vars["options"] = opts
		vars["options_json"] = bindingOptionsJSON(opts)
		s.renderPage(c, http.StatusOK, "eventrule_form.html", vars)
		return
	}
	SetFlash(c, "success", "Event rule updated")
	c.Redirect(http.StatusFound, "/admin/eventrules")
}

func (s *Server) AdminEventRuleDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.DisplayRule.DeleteOneID(id).Exec(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to delete: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/eventrules")
		return
	}
	SetFlash(c, "success", "Event rule deleted")
	c.Redirect(http.StatusFound, "/admin/eventrules")
}

// Ensure imports used
var _ = json.Marshal
var _ = template.JS("")
