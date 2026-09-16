package handlers

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/grandcat/zeroconf"
	"ledit/ent/devicesettings"
	"ledit/ent/generalsettings"
)

// DiscoveredDevice holds an mDNS-discovered candidate.
type DiscoveredDevice struct {
	Fingerprint string    `json:"fingerprint"`
	Model       string    `json:"model"`
	Version     string    `json:"version"`
	Proto       string    `json:"proto"`
	Nonce       string    `json:"nonce"`
	Address     string    `json:"address"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	State       string    `json:"state"`
}

const discoveryTTL = 2 * time.Minute

var (
	discoveryMu      sync.Mutex
	discoveryDevices = map[string]DiscoveredDevice{}
	discoveryStarted bool
)

func discoveryResetForTest() {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	discoveryDevices = map[string]DiscoveredDevice{}
	discoveryStarted = false
}

func discoverySweepLocked() {
	now := time.Now()
	for k, v := range discoveryDevices {
		if now.Sub(v.LastSeen) > discoveryTTL {
			delete(discoveryDevices, k)
		}
	}
}

func discoverySnapshot() []DiscoveredDevice {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	discoverySweepLocked()
	out := make([]DiscoveredDevice, 0, len(discoveryDevices))
	for _, v := range discoveryDevices {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].FirstSeen.After(out[j].FirstSeen)
	})
	return out
}

func discoveryEntry(fingerprint string) (DiscoveredDevice, bool) {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	discoverySweepLocked()
	v, ok := discoveryDevices[fingerprint]
	return v, ok
}

func discoveryUpsert(dev DiscoveredDevice) {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if existing, ok := discoveryDevices[dev.Fingerprint]; ok {
		dev.FirstSeen = existing.FirstSeen
		// preserve state unless explicitly set
		if dev.State == "" {
			dev.State = existing.State
		}
		if dev.State == "" {
			dev.State = "pending"
		}
	} else {
		if dev.FirstSeen.IsZero() {
			dev.FirstSeen = time.Now()
		}
		if dev.State == "" {
			dev.State = "pending"
		}
	}
	if dev.LastSeen.IsZero() {
		dev.LastSeen = time.Now()
	}
	discoveryDevices[dev.Fingerprint] = dev
}

func discoveryMarkApproved(fingerprint string) {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if v, ok := discoveryDevices[fingerprint]; ok {
		v.State = "approved"
		discoveryDevices[fingerprint] = v
	}
}

func discoveryMarkPending(fingerprint string) {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if v, ok := discoveryDevices[fingerprint]; ok {
		v.State = "pending"
		discoveryDevices[fingerprint] = v
	}
}

func discoveryNonceValid(fingerprint, nonce string) bool {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	v, ok := discoveryDevices[fingerprint]
	if !ok {
		return false
	}
	return v.Nonce != "" && v.Nonce == nonce
}

func discoveryInvalidateNonce(fingerprint string) {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if v, ok := discoveryDevices[fingerprint]; ok {
		v.Nonce = ""
		discoveryDevices[fingerprint] = v
	}
}

// StartDiscovery browses _ledit._tcp.local via mDNS.
func StartDiscovery(s *Server) {
	discoveryMu.Lock()
	if discoveryStarted {
		discoveryMu.Unlock()
		return
	}
	discoveryStarted = true
	discoveryMu.Unlock()

	go func() {
		resolver, err := zeroconf.NewResolver(nil)
		if err != nil {
			slog.Warn("mDNS discovery unavailable", "error", err)
			return
		}
		entries := make(chan *zeroconf.ServiceEntry)
		go func() {
			for e := range entries {
				if e == nil {
					continue
				}
				txtMap := map[string]string{}
				for _, t := range e.Text {
					if idx := strings.Index(t, "="); idx >= 0 {
						txtMap[t[:idx]] = t[idx+1:]
					}
				}
				fp := txtMap["id"]
				if fp == "" {
					continue
				}
				addr := ""
				if len(e.AddrIPv4) > 0 {
					addr = e.AddrIPv4[0].String()
				} else if len(e.AddrIPv6) > 0 {
					addr = e.AddrIPv6[0].String()
				}
				port := e.Port
				hostPort := addr
				if port != 0 && addr != "" {
					hostPort = net.JoinHostPort(addr, strings.TrimSpace(strconv.Itoa(port)))
				}
				now := time.Now()
				discoveryMu.Lock()
				existing, ok := discoveryDevices[fp]
				dev := DiscoveredDevice{
					Fingerprint: fp,
					Model:       txtMap["model"],
					Version:     txtMap["version"],
					Proto:       txtMap["proto"],
					Nonce:       txtMap["nonce"],
					Address:     hostPort,
					LastSeen:    now,
				}
				if ok {
					dev.FirstSeen = existing.FirstSeen
					dev.State = existing.State
				} else {
					dev.FirstSeen = now
					dev.State = "pending"
				}
				if dev.State == "" {
					dev.State = "pending"
				}
				discoveryDevices[fp] = dev
				discoveryMu.Unlock()

				// Mark known if DB has fingerprint
				if s != nil && s.DB != nil {
					exists, _ := s.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Exist(s.Ctx)
					if exists {
						discoveryMu.Lock()
						if v, ok := discoveryDevices[fp]; ok {
							v.State = "known"
							discoveryDevices[fp] = v
						}
						discoveryMu.Unlock()
					}
				}
			}
		}()
		if err := resolver.Browse(context.Background(), "_ledit._tcp", "local.", entries); err != nil {
			slog.Warn("mDNS browse failed", "error", err)
			return
		}
	}()
}

// AdminDeviceDiscovery returns discovered devices as JSON.
func (s *Server) AdminDeviceDiscovery(c *gin.Context) {
	devs := discoverySnapshot()
	if devs == nil {
		devs = []DiscoveredDevice{}
	}
	c.JSON(http.StatusOK, gin.H{"devices": devs})
}

// AdminDeviceEnroll creates a DeviceSettings row for a discovered device.
func (s *Server) AdminDeviceEnroll(c *gin.Context) {
	fp := c.Param("fingerprint")
	if fp == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint required"})
		return
	}
	dev, ok := discoveryEntry(fp)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	// Check existing DeviceSettings
	exists, err := s.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Exist(s.Ctx)
	if err == nil && exists {
		c.JSON(http.StatusConflict, gin.H{"error": "already enrolled"})
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = strings.TrimSpace(c.Query("name"))
	}
	if name == "" {
		// try JSON body
		var body struct {
			Name string `json:"name"`
		}
		_ = c.ShouldBindJSON(&body)
		if strings.TrimSpace(body.Name) != "" {
			name = strings.TrimSpace(body.Name)
		}
	}
	if name == "" {
		// fallback to model or fingerprint
		if dev.Model != "" {
			name = dev.Model
		} else {
			name = fp
		}
	}
	token := generateDeviceToken()
	obj, err := s.DB.DeviceSettings.Create().
		SetName(name).
		SetFingerprint(fp).
		SetApprovedAt(time.Now()).
		SetToken(token).
		Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create device"})
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		_ = s.DB.GeneralSettings.UpdateOne(settings).AddDeviceSettings(obj).Exec(s.Ctx)
	}
	discoveryMarkApproved(fp)
	c.JSON(http.StatusOK, gin.H{"id": obj.ID, "name": obj.Name, "fingerprint": fp})
}

// AdminDeviceEnrollmentCancel deletes unclaimed device and reverts discovery state.
func (s *Server) AdminDeviceEnrollmentCancel(c *gin.Context) {
	fp := c.Param("fingerprint")
	if fp == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint required"})
		return
	}
	ds, err := s.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Only(s.Ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	if ds.LastSeenAt != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "device already claimed"})
		return
	}
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		_ = s.DB.GeneralSettings.UpdateOne(settings).RemoveDeviceSettings(ds).Exec(s.Ctx)
	}
	_ = s.DB.DeviceSettings.DeleteOne(ds).Exec(s.Ctx)
	discoveryMarkPending(fp)
	c.JSON(http.StatusOK, gin.H{"status": "canceled"})
}

// DeviceProvision returns token for approved device with valid nonce.
func (s *Server) DeviceProvision(c *gin.Context) {
	if ok, retry := checkWindowRateLimit("provision:"+c.ClientIP(), 30); !ok {
		abortInboundRateLimited(c, retry)
		return
	}
	fp := c.Query("fingerprint")
	nonce := c.Query("nonce")
	if fp == "" {
		fp = c.PostForm("fingerprint")
	}
	if nonce == "" {
		nonce = c.PostForm("nonce")
	}
	if fp == "" || nonce == "" {
		c.Status(http.StatusNoContent)
		return
	}
	dev, ok := discoveryEntry(fp)
	if !ok {
		c.Status(http.StatusNoContent)
		return
	}
	if !discoveryNonceValid(fp, nonce) {
		c.Status(http.StatusNoContent)
		return
	}
	// Check source host matches discovery address host
	if dev.Address != "" {
		host, _, err := net.SplitHostPort(dev.Address)
		if err != nil {
			host = dev.Address
		}
		if host != "" && host != c.ClientIP() {
			// If discovery host is set, enforce match
			// But allow if ClientIP is empty (test) - skip
			if c.ClientIP() != "" {
				// compare host part
				if host != c.ClientIP() {
					c.Status(http.StatusNoContent)
					return
				}
			}
		}
	}
	ds, err := s.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Only(s.Ctx)
	if err != nil || ds.ApprovedAt == nil {
		c.Status(http.StatusNoContent)
		return
	}
	// Return existing token, invalidate nonce
	token := ds.Token
	discoveryInvalidateNonce(fp)
	c.JSON(http.StatusOK, gin.H{"token": token, "server": c.Request.Host})
}

// AdminDiscoveredDevices renders the discovery admin page.
func (s *Server) AdminDiscoveredDevices(c *gin.Context) {
	devs := discoverySnapshot()
	if devs == nil {
		devs = []DiscoveredDevice{}
	}
	s.renderPage(c, http.StatusOK, "devices_discovered.html", gin.H{
		"discovered": devs,
		"active":     "discovery",
	})
}

// TestSeedDiscovery is test-only (LEDIT_AUTH_DISABLE): seeds a pending discovered device.
func (s *Server) TestSeedDiscovery(c *gin.Context) {
	var req struct {
		Fingerprint string `json:"fingerprint" form:"fingerprint"`
		Model       string `json:"model" form:"model"`
		Version     string `json:"version" form:"version"`
		Proto       string `json:"proto" form:"proto"`
		Nonce       string `json:"nonce" form:"nonce"`
		Address     string `json:"address" form:"address"`
		Clear       bool   `json:"clear" form:"clear"`
	}
	_ = c.ShouldBind(&req)
	if req.Fingerprint == "" {
		req.Fingerprint = c.Query("fingerprint")
	}
	if req.Model == "" {
		req.Model = c.Query("model")
	}
	if req.Version == "" {
		req.Version = c.Query("version")
	}
	if req.Proto == "" {
		req.Proto = c.Query("proto")
	}
	if req.Nonce == "" {
		req.Nonce = c.Query("nonce")
	}
	if req.Address == "" {
		req.Address = c.Query("address")
	}
	if req.Clear || req.Fingerprint == "" {
		if c.Query("clear") == "true" || c.Query("clear") == "1" || req.Clear {
			discoveryResetForTest()
			c.JSON(http.StatusOK, gin.H{"status": "reset"})
			return
		}
		if req.Fingerprint == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint required"})
			return
		}
	}
	// reset/overwrite fingerprint entry: delete then upsert to force pending
	discoveryMu.Lock()
	delete(discoveryDevices, req.Fingerprint)
	discoveryMu.Unlock()
	discoveryUpsert(DiscoveredDevice{
		Fingerprint: req.Fingerprint,
		Model:       req.Model,
		Version:     req.Version,
		Proto:       req.Proto,
		Nonce:       req.Nonce,
		Address:     req.Address,
		State:       "pending",
	})
	c.JSON(http.StatusOK, gin.H{"status": "seeded", "fingerprint": req.Fingerprint})
}
