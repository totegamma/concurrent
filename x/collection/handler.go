package collection

import (
	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"go.opentelemetry.io/otel"
	"io/ioutil"
	"log"
	"net/http"
)

var tracer = otel.Tracer("collection")

type IHandler interface {
	CreateCollection(c echo.Context) error
	GetCollection(c echo.Context) error
	UpdateCollection(c echo.Context) error
	DeleteCollection(c echo.Context) error

	CreateItem(c echo.Context) error
	GetItem(c echo.Context) error
	UpdateItem(c echo.Context) error
	DeleteItem(c echo.Context) error
}

type Handler struct {
	service IService
}

func NewHandler(service IService) IHandler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) CreateCollection(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerCreateCollection")
	defer span.End()

	var data core.Collection
	err := c.Bind(&data)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": err.Error()})
	}

	created, err := h.service.CreateCollection(ctx, data)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusCreated, echo.Map{"status": "ok", "content": created})
}

func (h *Handler) GetCollection(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetCollection")
	defer span.End()

	id := c.Param("id")

	data, err := h.service.GetCollection(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": data})
}

func (h *Handler) UpdateCollection(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdateCollection")
	defer span.End()

	id := c.Param("id")

	collection := core.Collection{}
	err := c.Bind(&collection)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request", "message": err.Error()})
	}

	collection.ID = id

	updated, err := h.service.UpdateCollection(ctx, collection)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": updated})
}

func (h *Handler) DeleteCollection(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDeleteCollection")
	defer span.End()

	id := c.Param("id")

	err := h.service.DeleteCollection(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

func (h *Handler) CreateItem(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerCreateItem")
	defer span.End()

	collectionID := c.Param("collection")

	body := c.Request().Body
	bytes, err := ioutil.ReadAll(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	value := string(bytes)

	created, err := h.service.CreateItem(ctx, core.CollectionItem{
		Collection: collectionID,
		Payload:    value,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusCreated, echo.Map{"status": "ok", "content": created})
}

func (h *Handler) GetItem(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetItem")
	defer span.End()

	collectionID := c.Param("collection")
	id := c.Param("id")

	data, err := h.service.GetItem(ctx, collectionID, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": data})
}

func (h *Handler) UpdateItem(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerUpdateItem")
	defer span.End()

	collectionID := c.Param("collection")
	itemID := c.Param("item")

	body := c.Request().Body
	bytes, err := ioutil.ReadAll(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	value := string(bytes)

	updated, err := h.service.UpdateItem(ctx, core.CollectionItem{
		ID:         itemID,
		Collection: collectionID,
		Payload:    value,
	})

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": updated})
}

func (h *Handler) DeleteItem(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDeleteItem")
	defer span.End()

	collectionID := c.Param("collection")
	itemID := c.Param("item")

	log.Println("Delete item", collectionID, itemID)

	deleted, err := h.service.DeleteItem(ctx, collectionID, itemID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": deleted})
}
