package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// package-level controllers for restart-on-save
var mqttCtrl *MQTTController
var tgBot *TelegramBot

func (s *Server) AdminWebhookSettingsGET(c *gin.Context) {
	settings, err := s.DB.WebhookSettings.Query().Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	s.renderPage(c, http.StatusOK, "webhook.html", gin.H{
		"settings": settings,
	})
}

func (s *Server) AdminWebhookSettingsPOST(c *gin.Context) {
	apiKey := strings.TrimSpace(c.PostForm("api_key"))
	ttlStr := strings.TrimSpace(c.PostForm("default_ttl"))
	ttl := 30
	if ttlStr != "" {
		if v, err := strconv.Atoi(ttlStr); err == nil {
			ttl = v
		}
	}
	if ttl < 1 {
		ttl = 1
	}
	if ttl > 3600 {
		ttl = 3600
	}
	exists, _ := s.DB.WebhookSettings.Query().Exist(s.Ctx)
	if !exists {
		s.DB.WebhookSettings.Create().SetAPIKey(apiKey).SetDefaultTTL(ttl).SaveX(s.Ctx)
	} else {
		s.DB.WebhookSettings.Update().SetAPIKey(apiKey).SetDefaultTTL(ttl).SaveX(s.Ctx)
	}
	SetFlash(c, "success", "Webhook settings saved")
	c.Redirect(http.StatusFound, "/admin/webhook")
}

func (s *Server) AdminMQTTSettingsGET(c *gin.Context) {
	settings, err := s.DB.MQTTSettings.Query().Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	s.renderPage(c, http.StatusOK, "mqtt.html", gin.H{
		"settings": settings,
	})
}

func (s *Server) AdminMQTTSettingsPOST(c *gin.Context) {
	enabled := c.PostForm("enabled") == "on"
	broker := strings.TrimSpace(c.PostForm("broker"))
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	controlTopic := strings.TrimSpace(c.PostForm("control_topic"))
	if controlTopic == "" {
		controlTopic = "ledit/control"
	}
	displayTopic := strings.TrimSpace(c.PostForm("display_topic"))
	if displayTopic == "" {
		displayTopic = "ledit/display"
	}
	if enabled && broker == "" {
		settings, _ := s.DB.MQTTSettings.Query().Only(s.Ctx)
		s.renderPage(c, http.StatusOK, "mqtt.html", gin.H{
			"settings": settings,
			"error":    "Broker is required when MQTT is enabled",
		})
		return
	}
	exists, _ := s.DB.MQTTSettings.Query().Exist(s.Ctx)
	if !exists {
		s.DB.MQTTSettings.Create().SetEnabled(enabled).SetBroker(broker).SetUsername(username).SetPassword(password).SetControlTopic(controlTopic).SetDisplayTopic(displayTopic).SaveX(s.Ctx)
	} else {
		s.DB.MQTTSettings.Update().SetEnabled(enabled).SetBroker(broker).SetUsername(username).SetPassword(password).SetControlTopic(controlTopic).SetDisplayTopic(displayTopic).SaveX(s.Ctx)
	}
	// restart MQTT
	if mqttCtrl != nil {
		mqttCtrl = mqttCtrl.RestartWithSettings(s)
	} else {
		mqttCtrl = StartMQTT(s)
	}
	SetFlash(c, "success", "MQTT settings saved")
	c.Redirect(http.StatusFound, "/admin/mqtt")
}

func (s *Server) AdminTelegramSettingsGET(c *gin.Context) {
	settings, err := s.DB.TelegramSettings.Query().Only(s.Ctx)
	if err != nil {
		settings = nil
	}
	s.renderPage(c, http.StatusOK, "telegram.html", gin.H{
		"settings": settings,
	})
}

func (s *Server) AdminTelegramSettingsPOST(c *gin.Context) {
	enabled := c.PostForm("enabled") == "on"
	botToken := strings.TrimSpace(c.PostForm("bot_token"))
	allowedStr := strings.TrimSpace(c.PostForm("allowed_chat_id"))
	var allowed int64
	if allowedStr != "" {
		if v, err := strconv.ParseInt(allowedStr, 10, 64); err == nil {
			allowed = v
		}
	}
	if enabled && botToken == "" {
		settings, _ := s.DB.TelegramSettings.Query().Only(s.Ctx)
		s.renderPage(c, http.StatusOK, "telegram.html", gin.H{
			"settings": settings,
			"error":    "Bot token is required when Telegram is enabled",
		})
		return
	}
	exists, _ := s.DB.TelegramSettings.Query().Exist(s.Ctx)
	if !exists {
		s.DB.TelegramSettings.Create().SetEnabled(enabled).SetBotToken(botToken).SetAllowedChatID(allowed).SaveX(s.Ctx)
	} else {
		s.DB.TelegramSettings.Update().SetEnabled(enabled).SetBotToken(botToken).SetAllowedChatID(allowed).SaveX(s.Ctx)
	}
	if tgBot != nil {
		tgBot = tgBot.RestartWithSettings(s)
	} else {
		tgBot = StartTelegram(s)
	}
	SetFlash(c, "success", "Telegram settings saved")
	c.Redirect(http.StatusFound, "/admin/telegram")
}
