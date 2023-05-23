package host

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/util"
)

// Handler is handles websocket
type Handler struct {
    service Service
    config util.Config
}

// NewHandler is used for wire.go
func NewHandler(service Service, config util.Config) Handler {
    return Handler{service, config}
}

// Get returns a host by ID
func (h Handler) Get(c echo.Context) error {
    id := c.Param("id")
    host := h.service.Get(id)
    return c.JSON(http.StatusOK, host)

}

// Upsert updates Host registry
func (h Handler) Upsert(c echo.Context) error {
    var host Host
    err := c.Bind(&host)
    if (err != nil) {
        return err
    }
    h.service.Upsert(&host)
    return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// List returns all hosts
func (h Handler) List(c echo.Context) error {
    hosts := h.service.List()
    return c.JSON(http.StatusOK, hosts)
}

// Profile returns server Profile
func (h Handler) Profile(c echo.Context) error {
    return c.JSON(http.StatusOK, Profile{
        ID: h.config.FQDN,
        CCAddr: h.config.CCAddr,
        Pubkey: h.config.Pubkey,
    })
}

// Hello iniciates a new host registration
func (h Handler) Hello(c echo.Context) error {
    var newcomer Profile
    err := c.Bind(&newcomer)
    if err != nil {
        return err
    }

    // challenge
    req, err := http.NewRequest("GET", "https://" + newcomer.ID + "/host", nil)
    if err != nil {
        return c.String(http.StatusBadRequest, err.Error())
    }
    client := new(http.Client)
    resp, err := client.Do(req)
    if err != nil {
        return c.String(http.StatusBadRequest, err.Error())
    }
    defer resp.Body.Close()

    body, _ := ioutil.ReadAll(resp.Body)

    var fetchedProf Profile
    json.Unmarshal(body, &fetchedProf)

    if newcomer.ID != fetchedProf.ID {
        return c.String(http.StatusBadRequest, "validation failed")
    }

    h.service.Upsert(&Host{
        ID: newcomer.ID,
        CCAddr: newcomer.CCAddr,
        Role: "unassigned",
        Pubkey: newcomer.Pubkey,
    })

    return c.JSON(http.StatusOK, Profile{
        ID: h.config.FQDN,
        CCAddr: h.config.CCAddr,
        Pubkey: h.config.Pubkey,
    })
}

// SayHello iniciates a new host registration
func (h Handler) SayHello(c echo.Context) error {
    target := c.Param("fqdn")

    me := Profile{
        ID: h.config.FQDN,
        CCAddr: h.config.CCAddr,
        Pubkey: h.config.Pubkey,
    }

    meStr, err := json.Marshal(me)

    // challenge
    req, err := http.NewRequest("POST", "https://" + target + "/api/v1/host/hello", bytes.NewBuffer(meStr))
    if err != nil {
        return c.String(http.StatusBadRequest, err.Error())
    }
    req.Header.Add("content-type", "application/json")
    client := new(http.Client)
    resp, err := client.Do(req)
    if err != nil {
        return c.String(http.StatusBadRequest, err.Error())
    }
    defer resp.Body.Close()

    body, _ := ioutil.ReadAll(resp.Body)

    var fetchedProf Profile
    json.Unmarshal(body, &fetchedProf)

    h.service.Upsert(&Host{
        ID: fetchedProf.ID,
        CCAddr: fetchedProf.CCAddr,
        Role: "unassigned",
        Pubkey: fetchedProf.Pubkey,
    })

    return c.JSON(http.StatusOK, fetchedProf)
}

