package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/gin-gonic/gin"
)

var flashHMACKey []byte

func init() {
	key := make([]byte, 32)
	rand.Read(key)
	flashHMACKey = key
}

type flashMsg struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func signFlash(data []byte) string {
	mac := hmac.New(sha256.New, flashHMACKey)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// SetFlash stores a flash message in a signed cookie.
func SetFlash(c *gin.Context, msgType, msg string) {
	f := flashMsg{Type: msgType, Message: msg}
	data, _ := json.Marshal(f)
	sig := signFlash(data)
	val := hex.EncodeToString(data) + "." + sig
	c.SetCookie("flash", val, 0, "/", "", false, true)
}

// GetFlash retrieves and clears flash messages from the cookie.
func GetFlash(c *gin.Context) *flashMsg {
	cookie, err := c.Cookie("flash")
	if err != nil {
		return nil
	}
	c.SetCookie("flash", "", -1, "/", "", false, true)

	dot := -1
	for i, ch := range cookie {
		if ch == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		return nil
	}

	hexData := cookie[:dot]
	sig := cookie[dot+1:]

	data, err := hex.DecodeString(hexData)
	if err != nil {
		return nil
	}

	if signFlash(data) != sig {
		return nil
	}

	var f flashMsg
	if err := json.Unmarshal(data, &f); err != nil {
		return nil
	}
	return &f
}

// FlashMiddleware injects flash message into the template context.
func FlashMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if flash := GetFlash(c); flash != nil {
			c.Set("flash_type", flash.Type)
			c.Set("flash_message", flash.Message)
		}
		c.Next()
	}
}
