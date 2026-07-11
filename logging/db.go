package logging

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TraceDBQuery wraps a database query function with an OTel span.
// It creates a span named after the operation, executes dbFunc within it,
// and records any error on the span.
func TraceDBQuery(ctx context.Context, operation string, dbFunc func(ctx context.Context) error) error {
	tracer := otel.Tracer("ledit")
	ctx, span := tracer.Start(ctx, operation,
		trace.WithAttributes(attribute.String("db.operation", operation)),
	)
	defer span.End()

	if err := dbFunc(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}
