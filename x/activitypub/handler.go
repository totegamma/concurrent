// Package activitypub provides an ActivityPub server.
package activitypub

import (
    "strings"
    "net/http"
    "github.com/labstack/echo/v4"
    "github.com/totegamma/concurrent/x/util"
)

// Handler is a handler for the WebFinger protocol.
type Handler struct {
    repo *Repository
    config util.Config
}

// NewHandler returns a new Handler.
func NewHandler(repo *Repository, config util.Config) *Handler {
    return &Handler{repo, config}
}

// WebFinger handles WebFinger requests.
func (h Handler) WebFinger(c echo.Context) error {
    resource := c.QueryParam("resource")
    split := strings.Split(resource, ":")
    if len(split) != 2 {
        return c.String(http.StatusBadRequest, "Invalid resource")
    }
    rt, id := split[0], split[1]
    if rt != "acct" {
        return c.String(http.StatusBadRequest, "Invalid resource")
    }
    split = strings.Split(id, "@")
    if len(split) != 2 {
        return c.String(http.StatusBadRequest, "Invalid resource")
    }
    username, domain := split[0], split[1]
    if domain != h.config.FQDN {
        return c.String(http.StatusBadRequest, "Invalid resource")
    }

    _, err := h.repo.GetEntityByID(username)
    if err != nil {
        return c.String(http.StatusNotFound, "entity not found")
    }

    return c.JSON(http.StatusOK, WebFinger{
        Subject: resource,
        Links: []WebFingerLink{
            {
                Rel: "self",
                Type: "application/activity+json",
                Href: "https://" + h.config.FQDN + "/ap/" + username,
            },
        },
    })
}

// User handles user requests.
func (h Handler) User(c echo.Context) error {
    id := c.Param("id")
    if id == "" {
        return c.String(http.StatusBadRequest, "Invalid username")
    }

    entity, err := h.repo.GetEntityByID(id)
    if err != nil {
        return c.String(http.StatusNotFound, "entity not found")
    }

    return c.JSON(http.StatusOK, ActivityPub{
        Context: "https://www.w3.org/ns/activitystreams",
        Type: "Person",
        ID: "https://" + h.config.FQDN + "/ap/" + id,
        Inbox: "https://" + h.config.FQDN + "/ap/" + id + "/inbox",
        Outbox: "https://" + h.config.FQDN + "/ap/" + id + "/outbox",
        Followers: "https://" + h.config.FQDN + "/ap/" + id + "/followers",
        Following: "https://" + h.config.FQDN + "/ap/" + id + "/following",
        Liked: "https://" + h.config.FQDN + "/ap/" + id + "/liked",
        PreferredUsername: id,
        Name: entity.Name,
        Summary: entity.Summary,
        URL: entity.ProfileURL,
        Icon: Icon{
            Type: "Image",
            MediaType: "image/png",
            URL: entity.IconURL,
        },
    })
}


// UpdateEntity handles entity updates.
func (h Handler) UpdateEntity(c echo.Context) error {

    var entity Entity

    err := c.Bind(&entity)
    if err != nil {
        return c.String(http.StatusBadRequest, "Invalid request body")
    }

    created, err := h.repo.UpsertEntity(entity)
    if err != nil {
        return c.String(http.StatusInternalServerError, "Internal server error")
    }

    return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": created})
}

