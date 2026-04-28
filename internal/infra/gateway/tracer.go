package gateway

import (
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("gateway")
