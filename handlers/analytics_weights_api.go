package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) APIAnalyticsWeights(c *gin.Context) {
	w, d, sk, computedAt, cfg := globalWeightsCache.GetSnapshot()
	// fallback to equal weights if empty
	totalDisplays := 0
	for _, v := range d {
		totalDisplays += v
	}
	collecting := totalDisplays < 5*max(1, len(w))
	if w == nil || len(w) == 0 {
		collecting = true
	}
	// build response maps with string keys
	weights := map[string]float64{}
	displays := map[string]int{}
	skips := map[string]int{}
	for k, v := range w {
		key := k.Type
		if k.ID != 0 {
			key = k.Type + ":" + string(rune(k.ID))
		}
		weights[key] = v
	}
	for k, v := range d {
		weightsKey := k.Type
		displays[weightsKey] = v
	}
	for k, v := range sk {
		skips[k.Type] = v
	}
	// ensure sum 1.0 if collecting fallback equal weights
	if collecting && len(weights) == 0 {
		// no data, return empty with collecting flag
	}
	c.JSON(http.StatusOK, gin.H{
		"weights":             weights,
		"displays":            displays,
		"skips":               skips,
		"computedAt":          computedAt,
		"config":              cfg,
		"collectingData":      collecting,
		"floorClampedSources": []string{},
	})
}
