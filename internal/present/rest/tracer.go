package rest

import (
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("rest")
