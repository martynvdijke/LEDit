package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) ChartHistory(c *gin.Context) {
	typ := c.Param("type")
	id := c.Param("id")
	hoursStr := c.Query("hours")
	hours := 24
	if v, err := strconv.Atoi(hoursStr); err == nil && v > 0 {
		hours = v
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	rows, err := QueryHistory(c.Request.Context(), s.DB, typ, id, since)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}
