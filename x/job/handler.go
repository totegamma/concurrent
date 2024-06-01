package job

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("job")

type Handler interface {
	List(c echo.Context) error
	Create(c echo.Context) error
}

type handler struct {
	service core.JobService
}

func NewHandler(service core.JobService) Handler {
	return &handler{
		service: service,
	}
}

func (h *handler) List(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Job.Handler.List")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Unauthorized"})
	}

	jobs, err := h.service.List(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": jobs})
}

func (h *handler) Create(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Job.Handler.Create")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Unauthorized"})
	}

	var request Job
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	job, err := h.service.Create(ctx, requester, request.Type, request.Payload, request.Scheduled)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": job})
}
