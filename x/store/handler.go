package store

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("store")

type Handler interface {
	Commit(c echo.Context) error
}

type handler struct {
	service Service
}

func NewHandler(service Service) Handler {
	return &handler{
		service: service,
	}
}

func (h *handler) Commit(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "store.handler.Commit")
	defer span.End()

	var request commitRequest
	err := c.Bind(&request)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	result, err := h.service.Commit(ctx, request.Document, request.Signature)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": result})
}
