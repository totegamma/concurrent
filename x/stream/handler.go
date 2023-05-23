// Package stream is for handling concurrent stream object
package stream

import (
    "fmt"
    "strings"
    "net/http"
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
    stream := h.service.Get(streamStr)
    return c.JSON(http.StatusOK, stream)
}

// Post is for handling HTTP Post Method
func (h Handler) Post(c echo.Context) error {
    var query postQuery
    err := c.Bind(&query)
    if (err != nil) {
        return err
    }

    id := h.service.Post(query.Stream, query.ID, "")
    return c.String(http.StatusCreated, fmt.Sprintf("{\"message\": \"accept\", \"id\": \"%s\"}", id))

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
    messages := h.service.GetRecent(streams, 16)

    return c.JSON(http.StatusOK, messages)
}

// Range returns messages since to until in specified streams
func (h Handler) Range(c echo.Context) error {
    queryStreams := c.QueryParam("streams")
    streams := strings.Split(queryStreams, ",")
    querySince := c.QueryParam("streams")
    queryUntil := c.QueryParam("until")

    since := "-"
    if (querySince != "") {
        since = querySince
    }

    until := "+"
    if (queryUntil != "") {
        until = queryUntil
    }

    messages := h.service.GetRange(streams, since, until, 16)
    return c.JSON(http.StatusOK, messages)
}

// List returns stream ids which filtered by specific schema
func (h Handler) List(c echo.Context) error {
    schema := c.QueryParam("schema")
    list := h.service.StreamListBySchema(schema)
    return c.JSON(http.StatusOK, list)
}

// Checkpoint used by cross server communication
func (h Handler) Checkpoint(c echo.Context) error {
    var packet checkpointPacket
    err := c.Bind(&packet)
    if err != nil {
        return err
    }

    h.service.Post(packet.Stream, packet.ID, packet.Author)

    return c.String(http.StatusCreated, fmt.Sprintf("{\"message\": \"accept\"}"))
}

