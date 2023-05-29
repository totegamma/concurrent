package host

import (
    "bytes"
    "errors"
    "net/http"
    "io/ioutil"
    "encoding/json"

    "gorm.io/gorm"
    "golang.org/x/exp/slices"

    "github.com/labstack/echo/v4"
    "github.com/totegamma/concurrent/x/core"
    "github.com/totegamma/concurrent/x/util"
)

// Handler is handles websocket
type Handler struct {
    service *Service
    config util.Config
}

// NewHandler is used for wire.go
func NewHandler(service *Service, config util.Config) *Handler {
    return &Handler{service, config}
}

// Get returns a host by ID
func (h Handler) Get(c echo.Context) error {
    id := c.Param("id")
    host, err := h.service.Get(id)
    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return c.JSON(http.StatusNotFound, echo.Map{"error": "Host not found"})
        }
        return err
    }
    return c.JSON(http.StatusOK, host)

}

// Upsert updates Host registry
func (h Handler) Upsert(c echo.Context) error {
    var host core.Host
    err := c.Bind(&host)
    if (err != nil) {
        return err
    }
    err = h.service.Upsert(&host)
    if err != nil {
        return err
    }
    return c.String(http.StatusCreated, "{\"message\": \"accept\"}")
}

// List returns all hosts
func (h Handler) List(c echo.Context) error {
    hosts, err := h.service.List()
    if err != nil {
        return err
    }
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
    req, err := http.NewRequest("GET", "https://" + newcomer.ID + "/api/v1/host", nil)
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

    h.service.Upsert(&core.Host{
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
// Only Admin can call this
func (h Handler) SayHello(c echo.Context) error {
    target := c.Param("fqdn")

    claims := c.Get("jwtclaims").(util.JwtClaims)
    if !slices.Contains(h.config.Admins, claims.Audience) {
        return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action"})
    }

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

    h.service.Upsert(&core.Host{
        ID: fetchedProf.ID,
        CCAddr: fetchedProf.CCAddr,
        Role: "unassigned",
        Pubkey: fetchedProf.Pubkey,
    })

    return c.JSON(http.StatusOK, fetchedProf)
}

