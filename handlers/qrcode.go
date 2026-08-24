package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/qrcode"
)

func qrcodeFormFields(c *gin.Context) (q *datasource.QRSource, errMsg string) {
	content := c.PostForm("content")
	mode := c.PostForm("mode")
	if mode == "" {
		mode = "text"
	}
	wifiSSID := c.PostForm("wifi_ssid")
	wifiPassword := c.PostForm("wifi_password")
	wifiAuth := c.PostForm("wifi_auth")
	if wifiAuth == "" {
		wifiAuth = "WPA"
	}
	caption := c.PostForm("caption")
	ecc := c.PostForm("error_correction")
	if ecc == "" {
		ecc = "M"
	}
	qzStr := c.PostForm("quiet_zone")
	qz := 4
	if qzStr != "" {
		v, err := strconv.Atoi(qzStr)
		if err != nil {
			return nil, "quiet_zone must be an integer"
		}
		qz = v
	}
	qs := &datasource.QRSource{
		Content:         content,
		Mode:            mode,
		WifiSSID:        wifiSSID,
		WifiPassword:    wifiPassword,
		WifiAuth:        wifiAuth,
		Caption:         caption,
		ErrorCorrection: ecc,
		QuietZone:       qz,
	}
	if err := qs.Validate(); err != nil {
		return nil, err.Error()
	}
	return qs, ""
}

func (s *Server) AdminQrcodeList(c *gin.Context) {
	items, err := s.DB.Qrcode.Query().Order(ent.Asc(qrcode.FieldID)).All(s.Ctx)
	if err != nil {
		items = []*ent.Qrcode{}
	}
	s.renderPage(c, http.StatusOK, "qrcodes.html", gin.H{"qrcodes": items})
}

func (s *Server) AdminQrcodeNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "qrcode_form.html", gin.H{})
}

func (s *Server) AdminQrcodeCreate(c *gin.Context) {
	qs, msg := qrcodeFormFields(c)
	if msg != "" {
		SetFlash(c, "danger", msg)
		s.renderPage(c, http.StatusOK, "qrcode_form.html", gin.H{"obj": c.Request.PostForm, "error": msg})
		return
	}
	obj, err := s.DB.Qrcode.Create().
		SetContent(qs.Content).SetMode(qrcode.Mode(qs.Mode)).
		SetWifiSsid(qs.WifiSSID).SetWifiPassword(qs.WifiPassword).SetWifiAuth(qrcode.WifiAuth(qs.WifiAuth)).
		SetCaption(qs.Caption).SetErrorCorrection(qrcode.ErrorCorrection(qs.ErrorCorrection)).SetQuietZone(qs.QuietZone).
		Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		s.renderPage(c, http.StatusOK, "qrcode_form.html", gin.H{"error": err.Error()})
		return
	}
	if gs, err := s.DB.GeneralSettings.Query().Only(s.Ctx); err == nil && gs != nil {
		s.DB.GeneralSettings.UpdateOne(gs).AddQrcodes(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "QR code created")
	c.Redirect(http.StatusFound, "/admin/qrcodes")
}

func (s *Server) AdminQrcodeEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Qrcode.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "QR code not found")
		c.Redirect(http.StatusFound, "/admin/qrcodes")
		return
	}
	s.renderPage(c, http.StatusOK, "qrcode_form.html", gin.H{"obj": obj, "edit": true})
}

func (s *Server) AdminQrcodeUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	qs, msg := qrcodeFormFields(c)
	if msg != "" {
		SetFlash(c, "danger", msg)
		s.renderPage(c, http.StatusOK, "qrcode_form.html", gin.H{"obj": c.Request.PostForm, "edit": true, "id": id, "error": msg})
		return
	}
	err := s.DB.Qrcode.UpdateOneID(id).
		SetContent(qs.Content).SetMode(qrcode.Mode(qs.Mode)).
		SetWifiSsid(qs.WifiSSID).SetWifiPassword(qs.WifiPassword).SetWifiAuth(qrcode.WifiAuth(qs.WifiAuth)).
		SetCaption(qs.Caption).SetErrorCorrection(qrcode.ErrorCorrection(qs.ErrorCorrection)).SetQuietZone(qs.QuietZone).
		Exec(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
		s.renderPage(c, http.StatusOK, "qrcode_form.html", gin.H{"edit": true, "error": err.Error()})
		return
	}
	SetFlash(c, "success", "QR code updated")
	c.Redirect(http.StatusFound, "/admin/qrcodes")
}

func (s *Server) AdminQrcodeDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Qrcode.DeleteOneID(id).Exec(s.Ctx)
	SetFlash(c, "success", "QR code deleted")
	c.Redirect(http.StatusFound, "/admin/qrcodes")
}

// API handlers for /api/qrcodes (JSON, session auth)
func (s *Server) APIQrcodeList(c *gin.Context) {
	items, _ := s.DB.Qrcode.Query().All(s.Ctx)
	c.JSON(http.StatusOK, items)
}
func (s *Server) APIQrcodeGet(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Qrcode.Get(s.Ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// include formatted payload
	payload := obj.Content
	if string(obj.Mode) == "wifi" {
		payload = datasource.FormatWifiPayload(string(obj.WifiAuth), obj.WifiSsid, obj.WifiPassword)
	}
	c.JSON(http.StatusOK, gin.H{"qrcode": obj, "payload": payload})
}
func (s *Server) APIQrcodeCreate(c *gin.Context) {
	var req struct {
		Content         string `json:"content"`
		Mode            string `json:"mode"`
		WifiSSID        string `json:"wifi_ssid"`
		WifiPassword    string `json:"wifi_password"`
		WifiAuth        string `json:"wifi_auth"`
		Caption         string `json:"caption"`
		ErrorCorrection string `json:"error_correction"`
		QuietZone       *int   `json:"quiet_zone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	if req.Mode == "" {
		req.Mode = "text"
	}
	if req.WifiAuth == "" {
		req.WifiAuth = "WPA"
	}
	if req.ErrorCorrection == "" {
		req.ErrorCorrection = "M"
	}
	qz := 4
	if req.QuietZone != nil {
		qz = *req.QuietZone
	}
	qs := &datasource.QRSource{
		Content: req.Content, Mode: req.Mode, WifiSSID: req.WifiSSID, WifiPassword: req.WifiPassword, WifiAuth: req.WifiAuth, Caption: req.Caption, ErrorCorrection: req.ErrorCorrection, QuietZone: qz,
	}
	if err := qs.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	obj, err := s.DB.Qrcode.Create().
		SetContent(qs.Content).SetMode(qrcode.Mode(qs.Mode)).
		SetWifiSsid(qs.WifiSSID).SetWifiPassword(qs.WifiPassword).SetWifiAuth(qrcode.WifiAuth(qs.WifiAuth)).
		SetCaption(qs.Caption).SetErrorCorrection(qrcode.ErrorCorrection(qs.ErrorCorrection)).SetQuietZone(qs.QuietZone).
		Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if gs, err := s.DB.GeneralSettings.Query().Only(s.Ctx); err == nil && gs != nil {
		s.DB.GeneralSettings.UpdateOne(gs).AddQrcodes(obj).Exec(s.Ctx)
	}
	c.JSON(http.StatusCreated, obj)
}
func (s *Server) APIQrcodeUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Content         string `json:"content"`
		Mode            string `json:"mode"`
		WifiSSID        string `json:"wifi_ssid"`
		WifiPassword    string `json:"wifi_password"`
		WifiAuth        string `json:"wifi_auth"`
		Caption         string `json:"caption"`
		ErrorCorrection string `json:"error_correction"`
		QuietZone       *int   `json:"quiet_zone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	if req.Mode == "" {
		req.Mode = "text"
	}
	if req.WifiAuth == "" {
		req.WifiAuth = "WPA"
	}
	if req.ErrorCorrection == "" {
		req.ErrorCorrection = "M"
	}
	qz := 4
	if req.QuietZone != nil {
		qz = *req.QuietZone
	}
	qs := &datasource.QRSource{
		Content: req.Content, Mode: req.Mode, WifiSSID: req.WifiSSID, WifiPassword: req.WifiPassword, WifiAuth: req.WifiAuth, Caption: req.Caption, ErrorCorrection: req.ErrorCorrection, QuietZone: qz,
	}
	if err := qs.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := s.DB.Qrcode.UpdateOneID(id).
		SetContent(qs.Content).SetMode(qrcode.Mode(qs.Mode)).
		SetWifiSsid(qs.WifiSSID).SetWifiPassword(qs.WifiPassword).SetWifiAuth(qrcode.WifiAuth(qs.WifiAuth)).
		SetCaption(qs.Caption).SetErrorCorrection(qrcode.ErrorCorrection(qs.ErrorCorrection)).SetQuietZone(qs.QuietZone).
		Exec(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	obj, _ := s.DB.Qrcode.Get(s.Ctx, id)
	c.JSON(http.StatusOK, obj)
}
func (s *Server) APIQrcodeDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := s.DB.Qrcode.DeleteOneID(id).Exec(s.Ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
