package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) MetricsHandler(c *gin.Context) {
	// check toggle
	if s.DB != nil {
		settings := EnsureOutboundSettings(s.DB)
		if settings != nil && !settings.MetricsEnabled {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
	}
	c.Header("Content-Type", "text/plain; version=0.0.4")
	counters, gauges, eventsTotal := GlobalMetricsSink.Snapshot()
	// derive snapshots for health? For now just metrics sink data
	out := ""
	out += "# HELP ledit_events_total Total events by type\n"
	out += "# TYPE ledit_events_total counter\n"
	for k, v := range eventsTotal {
		out += fmt.Sprintf("ledit_events_total{event_type=%q} %d\n", k, v)
	}
	out += "# HELP ledit_source_frames_total Frames rendered per source\n"
	out += "# TYPE ledit_source_frames_total counter\n"
	for k, v := range counters {
		// k already includes label formatting, render as is if starts with {
		out += fmt.Sprintf("ledit_source_frames_total%s %d\n", k, v)
	}
	out += "# HELP ledit_device_online Device online gauge\n"
	out += "# TYPE ledit_device_online gauge\n"
	for k, v := range gauges {
		if k == "ledit_feed_paused" {
			continue
		}
		out += fmt.Sprintf("ledit_device_online%s %g\n", k, v)
	}
	out += "# HELP ledit_feed_paused Feed paused gauge\n"
	out += "# TYPE ledit_feed_paused gauge\n"
	if v, ok := gauges["ledit_feed_paused"]; ok {
		out += fmt.Sprintf("ledit_feed_paused %g\n", v)
	} else {
		out += "ledit_feed_paused 0\n"
	}
	out += "# HELP ledit_source_errors_total Errors per source\n"
	out += "# TYPE ledit_source_errors_total counter\n"
	// reuse counters with error label? For now empty
	c.String(http.StatusOK, out)
}
