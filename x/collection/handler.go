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

// Handler is the interface for handling HTTP requests
type Handler interface {
	CreateCollection(c echo.Context) error
	GetCollection(c echo.Context) error
	UpdateCollection(c echo.Context) error
	DeleteCollection(c echo.Context) error

	CreateItem(c echo.Context) error
	GetItem(c echo.Context) error
	UpdateItem(c echo.Context) error
	DeleteItem(c echo.Context) error
}

type handler struct {
	service Service
}

// NewHandler creates a new handler
func NewHandler(service Service) Handler {
	return &handler{
		service: service,
	}
}

// CreateCollection creates a new collection
func (h *handler) CreateCollection(c echo.Context) error {
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

// GetCollection returns a collection by ID
func (h *handler) GetCollection(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerGetCollection")
	defer span.End()

	id := c.Param("id")

	data, err := h.service.GetCollection(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": data})
}

// UpdateCollection updates a collection
func (h *handler) UpdateCollection(c echo.Context) error {
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

// DeleteCollection deletes a collection
func (h *handler) DeleteCollection(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerDeleteCollection")
	defer span.End()

	id := c.Param("id")

	err := h.service.DeleteCollection(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

// CreateItem creates a new item
func (h *handler) CreateItem(c echo.Context) error {
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
		Document:   value,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusCreated, echo.Map{"status": "ok", "content": created})
}

// GetItem returns an item by ID
func (h *handler) GetItem(c echo.Context) error {
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

// UpdateItem updates an item
func (h *handler) UpdateItem(c echo.Context) error {
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
		Document:   value,
	})

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": updated})
}

// DeleteItem deletes an item
func (h *handler) DeleteItem(c echo.Context) error {
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
