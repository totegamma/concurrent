package interop

import (
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("interop")
