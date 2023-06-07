// Package activitypub provides an ActivityPub server.
package activitypub

import (
    "fmt"
    "strings"
    "net/http"
    "io/ioutil"
    "encoding/pem"
    "crypto/ed25519"
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

    person, err := h.repo.GetPersonByID(id)
    if err != nil {
        return c.String(http.StatusNotFound, "person not found")
    }

    return c.JSON(http.StatusOK, Person {
        Context: "https://www.w3.org/ns/activitystreams",
        Type: "Person",
        ID: "https://" + h.config.FQDN + "/ap/" + id,
        Inbox: "https://" + h.config.FQDN + "/ap/" + id + "/inbox",
        Outbox: "https://" + h.config.FQDN + "/ap/" + id + "/outbox",
        Followers: "https://" + h.config.FQDN + "/ap/" + id + "/followers",
        Following: "https://" + h.config.FQDN + "/ap/" + id + "/following",
        Liked: "https://" + h.config.FQDN + "/ap/" + id + "/liked",
        PreferredUsername: id,
        Name: person.Name,
        Summary: person.Summary,
        URL: person.ProfileURL,
        Icon: Icon{
            Type: "Image",
            MediaType: "image/png",
            URL: person.IconURL,
        },
        PublicKey: Key{
            ID: "https://" + h.config.FQDN + "/ap/" + id + "#main-key",
            Type: "Key",
            Owner: "https://" + h.config.FQDN + "/ap/" + id,
            PublicKeyPem: entity.Publickey,
        },
    })
}

// UpdatePerson handles entity updates.
func (h Handler) UpdatePerson(c echo.Context) error {

    claims := c.Get("jwtclaims").(util.JwtClaims)
    ccaddr := claims.Audience

    entity, err := h.repo.GetEntityByCCAddr(ccaddr)
    if err != nil {
        return c.String(http.StatusNotFound, "entity not found")
    }

    if (entity.CCAddr != ccaddr) {
        return c.String(http.StatusUnauthorized, "unauthorized")
    }

    var person ApPerson
    err = c.Bind(&person)
    if err != nil {
        return c.String(http.StatusBadRequest, "Invalid request body")
    }

    created, err := h.repo.UpsertPerson(person)
    if err != nil {
        return c.String(http.StatusInternalServerError, "Internal server error")
    }

    return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": created})
}

// CreateEntity handles entity creation.
func (h Handler) CreateEntity(c echo.Context) error {

    claims := c.Get("jwtclaims").(util.JwtClaims)
    ccaddr := claims.Audience

    var request CreateEntityRequest
    err := c.Bind(&request)
    if err != nil {
        return c.String(http.StatusBadRequest, "Invalid request body")
    }

    // check if entity already exists
    _, err = h.repo.GetEntityByCCAddr(ccaddr)
    if err == nil {
        return c.String(http.StatusBadRequest, "Entity already exists")
    }

    // create ed25519 keypair
    pub, priv, err := ed25519.GenerateKey(nil)
    q := pem.EncodeToMemory(&pem.Block{
        Type: "PUBLIC KEY",
        Bytes: pub,
    })
    p := pem.EncodeToMemory(&pem.Block{
        Type: "PRIVATE KEY",
        Bytes: priv,
    })

    created, err := h.repo.CreateEntity(ApEntity {
        ID: request.ID,
        CCAddr: ccaddr,
        Publickey: string(q),
        Privatekey: string(p),
    })
    if err != nil {
        return c.String(http.StatusInternalServerError, "Internal server error")
    }

    return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": created})
}


// GetEntityID handles entity id requests.
func (h Handler) GetEntityID(c echo.Context) error {
    ccaddr := c.Param("ccaddr")
    if ccaddr == "" {
        return c.String(http.StatusBadRequest, "Invalid username")
    }

    entity, err := h.repo.GetEntityByCCAddr(ccaddr)
    if err != nil {
        return c.String(http.StatusNotFound, "entity not found")
    }

    return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity.ID})
}

// Inbox handles inbox requests.
/*
func (h Handler) Inbox(c echo.Context) error {
    id := c.Param("id")
    if id == "" {
        return c.String(http.StatusBadRequest, "Invalid username")
    }

    _, err := h.repo.GetEntityByID(id)
    if err != nil {
        return c.String(http.StatusNotFound, "entity not found")
    }

    var object Object
    err = c.Bind(&object)
    if err != nil {
        return c.String(http.StatusBadRequest, "Invalid request body")
    }

    // handle follow requests
    if object.Type == "Follow" {
        accept := Accept{
            Context: "https://www.w3.org/ns/activitystreams",
            Type: "Accept",
            Actor: "https://" + h.config.FQDN + "/ap/" + id,
            Object: object,
        }
    }

    return c.String(http.StatusOK, "ok")
}
*/

// PrintRequest prints the request body.
func PrintRequest(c echo.Context) error {

    body := c.Request().Body
    bytes, err := ioutil.ReadAll(body)
    if err != nil {
        return c.String(http.StatusInternalServerError, "Internal server error")
    }
    fmt.Println(string(bytes))

    return c.String(http.StatusOK, "ok")
}

