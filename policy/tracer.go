package policy

import (
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("policy")
