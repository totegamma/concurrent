package residence

import (
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("usecase")
