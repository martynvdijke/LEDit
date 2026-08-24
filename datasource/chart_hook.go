package datasource

var ChartRecorder func(sourceType, sourceID string, value float64)

// Note: sourceID unknown in datasource layer; handlers will set via SetChartContext
var chartCtxType, chartCtxID string

func SetChartContext(t, id string) { chartCtxType, chartCtxID = t, id }
func RecordChartValue(v float64) {
	if ChartRecorder != nil && chartCtxType != "" {
		ChartRecorder(chartCtxType, chartCtxID, v)
	}
}
