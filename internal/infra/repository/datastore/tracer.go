package datastore

import "go.opentelemetry.io/otel"

var tracer = otel.Tracer("repository.datastore")
