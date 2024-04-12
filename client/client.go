package client

import (
	"bytes"
	"net/http"
	"context"
    "time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
    defaultTimeout = 10 * time.Second
)

var tracer = otel.Tracer("client")

func Commit(ctx context.Context, domain, body string) (*http.Response, error) {
	ctx, span := tracer.Start(ctx, "Client.Commit")
	defer span.End()

    req, err := http.NewRequest("POST", "https://"+domain+"/api/v1/commit", bytes.NewBuffer([]byte(body)))
    if err != nil {
        span.RecordError(err)
        return &http.Response{}, err
    }

    otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

    client := new(http.Client)
    client.Timeout = defaultTimeout
    resp, err := client.Do(req)
    if err != nil {
        span.RecordError(err)
        return &http.Response{}, err
    }

    return resp, nil
}


