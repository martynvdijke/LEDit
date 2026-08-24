package handlers

import (
	"context"
	"log/slog"
	"time"

	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/chartsample"
	"ledit/ent/generalsettings"
)

func RecordSample(ctx context.Context, client *ent.Client, sourceType, sourceID string, value float64, ohlc *[4]float64) error {
	if client == nil {
		return nil
	}
	c := client.ChartSample.Create().SetSourceType(sourceType).SetSourceID(sourceID).SetSampledAt(time.Now()).SetValue(value)
	if ohlc != nil {
		c = c.SetOpen(ohlc[0]).SetHigh(ohlc[1]).SetLow(ohlc[2]).SetClose(ohlc[3])
	}
	_, err := c.Save(ctx)
	if err != nil {
		slog.Warn("chart sample record failed", "type", sourceType, "id", sourceID, "error", err)
	}
	return err
}

func QueryHistory(ctx context.Context, client *ent.Client, sourceType, sourceID string, since time.Time) ([]*ent.ChartSample, error) {
	if client == nil {
		return nil, nil
	}
	return client.ChartSample.Query().
		Where(chartsample.SourceTypeEQ(sourceType), chartsample.SourceIDEQ(sourceID), chartsample.SampledAtGT(since)).
		Order(ent.Asc(chartsample.FieldSampledAt)).
		All(ctx)
}

func PurgeOldSamples(ctx context.Context, client *ent.Client) error {
	if client == nil {
		return nil
	}
	gs, err := client.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(ctx)
	var retentionHours = 48
	var maxPoints = 576
	if err == nil && gs != nil {
		if gs.ChartRetentionHours != 0 {
			retentionHours = gs.ChartRetentionHours
		}
		if gs.ChartMaxPointsPerSource != 0 {
			maxPoints = gs.ChartMaxPointsPerSource
		}
	}
	cutoff := time.Now().Add(-time.Duration(retentionHours) * time.Hour)
	deleted, err := client.ChartSample.Delete().Where(chartsample.SampledAtLT(cutoff)).Exec(ctx)
	if err != nil {
		return err
	}
	if deleted > 0 {
		slog.Info("chart purge deleted old samples", "deleted", deleted, "cutoff", cutoff)
	}
	// per-source cap trim
	// get distinct source keys
	samples, err := client.ChartSample.Query().Order(ent.Asc(chartsample.FieldSampledAt)).All(ctx)
	if err != nil {
		return err
	}
	type key struct{ t, id string }
	groups := map[key][]*ent.ChartSample{}
	for _, s := range samples {
		k := key{s.SourceType, s.SourceID}
		groups[k] = append(groups[k], s)
	}
	for k, list := range groups {
		if len(list) > maxPoints {
			toDelete := list[:len(list)-maxPoints]
			for _, s := range toDelete {
				_ = client.ChartSample.DeleteOneID(s.ID).Exec(ctx)
			}
			slog.Info("chart cap trim", "type", k.t, "id", k.id, "trimmed", len(toDelete))
		}
	}
	return nil
}

func InitChartRecording(client *ent.Client) {
	datasource.ChartRecorder = func(sourceType, sourceID string, value float64) {
		_ = RecordSample(context.Background(), client, sourceType, sourceID, value, nil)
	}
}

func StartChartPurgeLoop(ctx context.Context, client *ent.Client) context.CancelFunc {
	cctx, cancel := context.WithCancel(ctx)
	go func() {
		// on-startup purge
		_ = PurgeOldSamples(cctx, client)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-cctx.Done():
				return
			case <-ticker.C:
				_ = PurgeOldSamples(cctx, client)
			}
		}
	}()
	return cancel
}
