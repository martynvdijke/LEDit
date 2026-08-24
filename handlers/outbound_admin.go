package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Server) OutboundWebhooksList(c *gin.Context) {
	hooks, _ := s.DB.OutboundWebhook.Query().All(s.Ctx)
	c.JSON(200, hooks)
}
func (s *Server) OutboundWebhooksCreate(c *gin.Context) {
	var req struct {
		URL     string `json:"url"`
		Secret  string `json:"secret"`
		Enabled *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := ValidateWebhookURL(req.URL); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := ValidateSecret(req.Secret); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	count, _ := s.DB.OutboundWebhook.Query().Count(s.Ctx)
	if count >= 32 {
		c.JSON(400, gin.H{"error": "max 32 webhooks"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	h, err := s.DB.OutboundWebhook.Create().SetURL(req.URL).SetSecret(req.Secret).SetEnabled(enabled).Save(s.Ctx)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, h)
}
func (s *Server) OutboundWebhooksDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.OutboundWebhook.DeleteOneID(id).Exec(s.Ctx); err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	c.JSON(200, gin.H{"status": "ok"})
}
func (s *Server) OutboundWebhooksTest(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	h, err := s.DB.OutboundWebhook.Get(s.Ctx, id)
	if err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	if GlobalWebhookSink == nil {
		c.JSON(500, gin.H{"error": "sink not ready"})
		return
	}
	d := GlobalWebhookSink.SendTest(h)
	c.JSON(200, d)
}
func (s *Server) OutboundSettingsGet(c *gin.Context) {
	st := EnsureOutboundSettings(s.DB)
	c.JSON(200, st)
}
func (s *Server) OutboundSettingsPut(c *gin.Context) {
	var req struct {
		MqttPublishEnabled *bool `json:"mqtt_publish_enabled"`
		MetricsEnabled     *bool `json:"metrics_enabled"`
		WebhooksEnabled    *bool `json:"webhooks_enabled"`
		HaDiscoveryEnabled *bool `json:"ha_discovery_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	st := EnsureOutboundSettings(s.DB)
	if st == nil {
		c.JSON(500, gin.H{"error": "db"})
		return
	}
	upd := s.DB.OutboundSettings.UpdateOneID(st.ID)
	if req.MqttPublishEnabled != nil {
		upd = upd.SetMqttPublishEnabled(*req.MqttPublishEnabled)
		GlobalMqttSink.SetEnabled(*req.MqttPublishEnabled)
	}
	if req.MetricsEnabled != nil {
		upd = upd.SetMetricsEnabled(*req.MetricsEnabled)
		GlobalMetricsSink.SetEnabled(*req.MetricsEnabled)
	}
	if req.WebhooksEnabled != nil {
		upd = upd.SetWebhooksEnabled(*req.WebhooksEnabled)
		if GlobalWebhookSink != nil {
			GlobalWebhookSink.SetEnabled(*req.WebhooksEnabled)
		}
	}
	if req.HaDiscoveryEnabled != nil {
		upd = upd.SetHaDiscoveryEnabled(*req.HaDiscoveryEnabled)
	}
	updated, err := upd.Save(s.Ctx)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, updated)
}
func (s *Server) AdminEventsPage(c *gin.Context) {
	hooks, _ := s.DB.OutboundWebhook.Query().All(s.Ctx)
	st := EnsureOutboundSettings(s.DB)
	var deliveries []Delivery
	if GlobalWebhookSink != nil {
		deliveries = GlobalWebhookSink.Deliveries()
	}
	s.renderPage(c, 200, "events.html", gin.H{"hooks": hooks, "settings": st, "deliveries": deliveries})
}
