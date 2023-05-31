// Package stream is for handling concurrent stream object
package stream

import (
    "fmt"
    "errors"
    "strings"
    "net/http"

    "gorm.io/gorm"
    "github.com/labstack/echo/v4"
)

// Handler handles Stream objects
type Handler struct {
    service *Service
}

// NewHandler is for wire.go
func NewHandler(service *Service) *Handler {
    return &Handler{service: service}
}


// Get is for handling HTTP Get Method
func (h Handler) Get(c echo.Context) error {
    streamStr := c.QueryParam("stream")
    stream, err := h.service.Get(streamStr)
    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return c.JSON(http.StatusNotFound, echo.Map{"error": "User not found"})
        }
        return err
    }
    return c.JSON(http.StatusOK, stream)
}

// Put is for handling HTTP Put Method
func (h Handler) Put(c echo.Context) error {
    var request postRequest
    err := c.Bind(&request)
    if err != nil {
        return err
    }

    id, err := h.service.Upsert(request.SignedObject, request.Signature, request.ID)
    if err != nil {
        return err
    }
    return c.String(http.StatusCreated, fmt.Sprintf("{\"message\": \"accept\", \"id\": \"%s\"}", id))
}

// Recent returns recent messages in some streams
func (h Handler) Recent(c echo.Context) error {
    streamsStr := c.QueryParam("streams")
    streams := strings.Split(streamsStr, ",")
    messages, _ := h.service.GetRecent(streams, 16)

    return c.JSON(http.StatusOK, messages)
}

// Range returns messages since to until in specified streams
func (h Handler) Range(c echo.Context) error {
    queryStreams := c.QueryParam("streams")
    streams := strings.Split(queryStreams, ",")
    querySince := c.QueryParam("since")
    queryUntil := c.QueryParam("until")

    since := "-"
    if (querySince != "") {
        since = querySince
    }

    until := "+"
    if (queryUntil != "") {
        until = queryUntil
    }

    messages, _ := h.service.GetRange(streams, since, until, 16)
    return c.JSON(http.StatusOK, messages)
}

// List returns stream ids which filtered by specific schema
func (h Handler) List(c echo.Context) error {
    schema := c.QueryParam("schema")
    list, err := h.service.StreamListBySchema(schema)
    if err != nil {
        return err
    }
    return c.JSON(http.StatusOK, list)
}

// Checkpoint used by cross server communication
func (h Handler) Checkpoint(c echo.Context) error {
    var packet checkpointPacket
    err := c.Bind(&packet)
    if err != nil {
        return err
    }

    err = h.service.Post(packet.Stream, packet.ID, packet.Type, packet.Author, packet.Host)
    if err != nil {
        return nil
    }

    return c.String(http.StatusCreated, fmt.Sprintf("{\"message\": \"accept\"}"))
}

