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
	Cancel(c echo.Context) error
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

	requesterContext, ok := ctx.Value(core.RequesterContextCtxKey).(core.RequesterContext)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}
	requester := requesterContext.Entity.ID

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

	requesterContext, ok := ctx.Value(core.RequesterContextCtxKey).(core.RequesterContext)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}
	requester := requesterContext.Entity.ID

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

func (h *handler) Cancel(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Job.Handler.Cancel")
	defer span.End()

	id := c.Param("id")
	job, err := h.service.Cancel(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": job})
}
