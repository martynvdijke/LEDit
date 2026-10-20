package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// internalError logs the underlying error and returns a generic message so
// database, filesystem, and upstream details never reach the client.
func internalError(c *gin.Context, err error) {
	if err != nil {
		slog.Error("request failed", "path", c.Request.URL.Path, "error", err)
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
}
