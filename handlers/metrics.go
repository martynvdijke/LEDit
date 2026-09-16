package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// cardinality: labels are ONLY bounded values — source "<type>:<id>",
// device numeric id, event_type/surface/status closed enums.
// Never name, URL, text, or message body.

func (s *Server) MetricsHandler(c *gin.Context) {
	if s.DB != nil {
		settings := EnsureOutboundSettings(s.DB)
		if settings != nil && !settings.MetricsEnabled {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
	}
	c.Header("Content-Type", "text/plain; version=0.0.4")

	counters, gauges, eventsTotal := GlobalMetricsSink.Snapshot()
	healthSnap := Health.Snapshot()
	hits, misses := Health.CacheCounters()

	type devInfo struct {
		ID              int
		FramesServed    int
		LastSeenUnix    int64
		FirmwareVersion string
		Online          int
	}
	var devInfos []devInfo
	if s.DB != nil {
		if rows, err := s.DB.DeviceSettings.Query().All(s.Ctx); err == nil {
			sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
			for _, d := range rows {
				var ts int64
				if d.LastSeenAt != nil {
					ts = d.LastSeenAt.Unix()
				}
				online := 0
				if sh, ok := healthSnap[fmt.Sprintf("device:%d", d.ID)]; ok {
					if StatusOf(sh) != "red" {
						online = 1
					}
				}
				devInfos = append(devInfos, devInfo{ID: d.ID, FramesServed: d.FramesServed, LastSeenUnix: ts, FirmwareVersion: d.FirmwareVersion, Online: online})
			}
		}
	}
	sort.Slice(devInfos, func(i, j int) bool { return devInfos[i].ID < devInfos[j].ID })

	var sourceKeys []string
	for k := range healthSnap {
		if strings.HasPrefix(k, "device:") {
			continue
		}
		sourceKeys = append(sourceKeys, k)
	}
	sort.Strings(sourceKeys)

	var eventKeys []string
	for k := range eventsTotal {
		eventKeys = append(eventKeys, k)
	}
	sort.Strings(eventKeys)

	var transportFrameKeys []string
	var transportErrorKeys []string
	for k := range counters {
		if strings.HasPrefix(k, "ledit_transport_frames_total") {
			transportFrameKeys = append(transportFrameKeys, k)
		}
		if strings.HasPrefix(k, "ledit_transport_errors_total") {
			transportErrorKeys = append(transportErrorKeys, k)
		}
	}
	sort.Strings(transportFrameKeys)
	sort.Strings(transportErrorKeys)

	deliveriesAgg := map[string]int64{}
	if s.DB != nil {
		if rows, err := s.DB.DeliveryLog.Query().All(s.Ctx); err == nil {
			for _, r := range rows {
				status := string(r.Status)
				mapped := "failed"
				if status == "delivered" || status == "acked" {
					mapped = "success"
				}
				surface := string(r.Surface)
				key := surface + "\x00" + mapped
				deliveriesAgg[key]++
			}
		}
	}
	var deliveryKeys []string
	for k := range deliveriesAgg {
		deliveryKeys = append(deliveryKeys, k)
	}
	sort.Strings(deliveryKeys)

	var buf bytes.Buffer
	emit := func(name, help, typ string, lines []string) {
		buf.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
		buf.WriteString(fmt.Sprintf("# TYPE %s %s\n", name, typ))
		sort.Strings(lines)
		for _, l := range lines {
			buf.WriteString(l + "\n")
		}
	}

	{
		var lines []string
		for _, k := range deliveryKeys {
			parts := strings.SplitN(k, "\x00", 2)
			surface, status := parts[0], parts[1]
			lines = append(lines, fmt.Sprintf(`ledit_deliveries_total{surface=%q,status=%q} %d`, surface, status, deliveriesAgg[k]))
		}
		emit("ledit_deliveries_total", "Total deliveries by surface and status", "counter", lines)
	}
	{
		var lines []string
		for _, d := range devInfos {
			if d.FirmwareVersion == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf(`ledit_device_firmware_info{device=%q,version=%q} 1`, fmt.Sprint(d.ID), d.FirmwareVersion))
		}
		emit("ledit_device_firmware_info", "Device firmware version info", "gauge", lines)
	}
	{
		var lines []string
		for _, d := range devInfos {
			lines = append(lines, fmt.Sprintf(`ledit_device_frames_served_total{device=%q} %d`, fmt.Sprint(d.ID), d.FramesServed))
		}
		emit("ledit_device_frames_served_total", "Frames served per device", "counter", lines)
	}
	{
		var lines []string
		for _, d := range devInfos {
			lines = append(lines, fmt.Sprintf(`ledit_device_last_seen_timestamp_seconds{device=%q} %d`, fmt.Sprint(d.ID), d.LastSeenUnix))
		}
		emit("ledit_device_last_seen_timestamp_seconds", "Last seen timestamp per device", "gauge", lines)
	}
	{
		var lines []string
		for _, d := range devInfos {
			lines = append(lines, fmt.Sprintf(`ledit_device_online{device=%q} %d`, fmt.Sprint(d.ID), d.Online))
		}
		emit("ledit_device_online", "Device online gauge", "gauge", lines)
	}
	{
		emit("ledit_devices_total", "Total number of devices", "gauge", []string{fmt.Sprintf("ledit_devices_total %d", len(devInfos))})
	}
	{
		var lines []string
		for _, k := range eventKeys {
			lines = append(lines, fmt.Sprintf(`ledit_events_total{event_type=%q} %d`, k, eventsTotal[k]))
		}
		emit("ledit_events_total", "Total events by type", "counter", lines)
	}
	{
		val := 0.0
		if v, ok := gauges["ledit_feed_paused"]; ok {
			val = v
		}
		emit("ledit_feed_paused", "Feed paused gauge", "gauge", []string{fmt.Sprintf("ledit_feed_paused %g", val)})
	}
	{
		emit("ledit_matrix_cache_hits_total", "Matrix cache hits total", "counter", []string{fmt.Sprintf("ledit_matrix_cache_hits_total %d", hits)})
	}
	{
		emit("ledit_matrix_cache_misses_total", "Matrix cache misses total", "counter", []string{fmt.Sprintf("ledit_matrix_cache_misses_total %d", misses)})
	}
	{
		var lines []string
		for _, k := range sourceKeys {
			sh := healthSnap[k]
			lines = append(lines, fmt.Sprintf(`ledit_source_errors_total{source=%q} %d`, k, sh.Failures))
		}
		emit("ledit_source_errors_total", "Errors per source", "counter", lines)
	}
	{
		var lines []string
		for _, k := range sourceKeys {
			sh := healthSnap[k]
			lines = append(lines, fmt.Sprintf(`ledit_source_frames_total{source=%q} %d`, k, sh.Renders))
		}
		emit("ledit_source_frames_total", "Frames rendered per source", "counter", lines)
	}
	{
		var lines []string
		for _, k := range sourceKeys {
			sh := healthSnap[k]
			var ts int64
			if !sh.LastSuccessAt.IsZero() {
				ts = sh.LastSuccessAt.Unix()
			}
			lines = append(lines, fmt.Sprintf(`ledit_source_last_success_timestamp_seconds{source=%q} %d`, k, ts))
		}
		emit("ledit_source_last_success_timestamp_seconds", "Last success timestamp per source", "gauge", lines)
	}
	{
		var lines []string
		for _, k := range sourceKeys {
			sh := healthSnap[k]
			secs := sh.EWMADurationMs / 1000.0
			lines = append(lines, fmt.Sprintf(`ledit_source_render_duration_seconds{source=%q} %g`, k, secs))
		}
		emit("ledit_source_render_duration_seconds", "EWMA render duration per source", "gauge", lines)
	}
	{
		var lines []string
		for _, k := range transportErrorKeys {
			lines = append(lines, fmt.Sprintf("ledit_transport_errors_total %d", counters[k]))
		}
		emit("ledit_transport_errors_total", "Transport send errors", "counter", lines)
	}
	{
		var lines []string
		for _, k := range transportFrameKeys {
			lines = append(lines, fmt.Sprintf("ledit_transport_frames_total %d", counters[k]))
		}
		emit("ledit_transport_frames_total", "Frames sent via transport sinks", "counter", lines)
	}

	c.String(http.StatusOK, buf.String())
}
