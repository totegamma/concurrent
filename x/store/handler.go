package store

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("store")

type Handler interface {
	Commit(c echo.Context) error
	Get(c echo.Context) error
	Post(c echo.Context) error
}

type handler struct {
	service core.StoreService
}

func NewHandler(service core.StoreService) Handler {
	return &handler{
		service: service,
	}
}

func (h *handler) Commit(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Store.Handler.Commit")
	defer span.End()

	passportFromEchoContext, ok := ctx.Value(core.RequesterPassportKey).(string)
	if ok {
		span.SetAttributes(attribute.String("passportFromEchoContext", passportFromEchoContext))
	}
	passportFromContext, ok := ctx.Value(core.RequesterPassportKey).(string)
	if ok {
		span.SetAttributes(attribute.String("passportFromContext", passportFromContext))
	}

	var request core.Commit
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	// limit document size 8KB
	if len(request.Document) > 8192 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Document size is too large"})
	}

	keys, ok := ctx.Value(core.RequesterKeychainKey).([]core.Key)
	if !ok {
		keys = []core.Key{}
	}

	result, err := h.service.Commit(ctx, core.CommitModeExecute, request.Document, request.Signature, request.Option, keys)
	if err != nil {
		if errors.Is(err, core.ErrorPermissionDenied{}) {
			return c.JSON(http.StatusForbidden, echo.Map{"error": "Permission Denied"})
		}
		if errors.Is(err, core.ErrorAlreadyExists{}) {
			return c.JSON(http.StatusOK, echo.Map{"info": "Already Exists"})
		}
		if errors.Is(err, core.ErrorAlreadyDeleted{}) {
			return c.JSON(http.StatusOK, echo.Map{"info": "Already Deleted"})
		}

		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, echo.Map{"content": result})
}

func (h *handler) Get(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Store.Handler.Get")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Unauthorized"})
	}

	path := h.service.GetPath(ctx, requester)

	fmt.Printf("path: %s\n", path)

	return c.Attachment(path, "archive.log")
}

func (h *handler) Post(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Store.Handler.Post")
	defer span.End()

	body := c.Request().Body
	defer body.Close()

	from := c.QueryParam("from")
	result, err := h.service.Restore(ctx, body, from)

	if err != nil {
		span.RecordError(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"content": result})
}
