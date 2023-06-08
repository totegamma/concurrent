// Package activitypub provides an ActivityPub server.
package activitypub

import (
    "fmt"
    "time"
    "bytes"
    "strings"
    "net/url"
    "net/http"
    "io/ioutil"
    "encoding/pem"
    "encoding/json"
    "crypto/x509"
    "crypto/ed25519"
    "github.com/go-fed/httpsig"
    "github.com/labstack/echo/v4"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/message"
)

// Handler is a handler for the WebFinger protocol.
type Handler struct {
    repo *Repository
    message *message.Service
    config util.Config
}

// NewHandler returns a new Handler.
func NewHandler(repo *Repository, message *message.Service, config util.Config) *Handler {
    return &Handler{repo, message, config}
}


// :: Activitypub Related Functions ::

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
                Href: "https://" + h.config.FQDN + "/ap/acct/" + username,
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
        ID: "https://" + h.config.FQDN + "/ap/acct/" + id,
        Inbox: "https://" + h.config.FQDN + "/ap/acct/" + id + "/inbox",
        Outbox: "https://" + h.config.FQDN + "/ap/acct/" + id + "/outbox",
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
            ID: "https://" + h.config.FQDN + "/ap/key/" + id,
            Type: "Key",
            Owner: "https://" + h.config.FQDN + "/ap/acct/" + id,
            PublicKeyPem: entity.Publickey,
        },
    })
}

// Note handles note requests.
func (h Handler) Note(c echo.Context) error {
    id := c.Param("id")
    if id == "" {
        return c.String(http.StatusBadRequest, "Invalid noteID")
    }
    message, err := h.message.Get(id)
    if err != nil {
        return c.String(http.StatusNotFound, "message not found")
    }

    entity, err := h.repo.GetEntityByCCAddr(message.Author)
    if err != nil {
        return c.String(http.StatusNotFound, "entity not found")
    }

    return c.JSON(http.StatusOK, Note{
        Context: "https://www.w3.org/ns/activitystreams",
        Type: "Note",
        ID: "https://" + h.config.FQDN + "/ap/note/" + id,
        AttributedTo: "https://" + h.config.FQDN + "/ap/acct/" + entity.ID,
        Content: message.Payload,
        Published: message.CDate.Format(time.RFC3339),
        To: []string{"https://www.w3.org/ns/activitystreams#Public"},
    })
}

// Inbox handles inbox requests.
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
        requester, err := FetchPerson(object.Actor)
        if err != nil {
            return c.String(http.StatusInternalServerError, "Internal server error")
        }
        accept := Accept{
            Context: "https://www.w3.org/ns/activitystreams",
            ID: "https://" + h.config.FQDN + "/ap/acct/" + id + "/follows/" + url.PathEscape(requester.ID),
            Type: "Accept",
            Actor: "https://" + h.config.FQDN + "/ap/acct/" + id,
            Object: object,
        }

        json, err := json.Marshal(accept)
        if err != nil {
            return c.String(http.StatusInternalServerError, "Internal server error")
        }

        req, err := http.NewRequest("POST", requester.Inbox, bytes.NewBuffer(json))
        if err != nil {
            return c.String(http.StatusInternalServerError, "Internal server error")
        }
        req.Header.Set("Content-Type", "application/activity+json")
        req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
        client := new(http.Client)


        split := strings.Split(object.Object.(string), "/")
        userID := split[len(split)-1]

        entity, err := h.repo.GetEntityByID(userID)
        //load private from pem
        block, _ := pem.Decode([]byte(entity.Privatekey))
        if block == nil {
            return fmt.Errorf("failed to parse PEM block containing the key")
        }

        // parse ed25519 private key
        priv, err := x509.ParsePKCS8PrivateKey(block.Bytes)
        if err != nil {
            return fmt.Errorf("failed to parse DER encoded private key: " + err.Error())
        }

        prefs := []httpsig.Algorithm{httpsig.ED25519}
        digestAlgorithm := httpsig.DigestSha256
        // The "Date" and "Digest" headers must already be set on r, as well as r.URL.
        headersToSign := []string{httpsig.RequestTarget, "date", "digest"}
        signer, _, err := httpsig.NewSigner(prefs, digestAlgorithm, headersToSign, httpsig.Signature, 0)
        if err != nil {
            return err
        }
        err = signer.SignRequest(priv, "https://" + h.config.FQDN + "/ap/acct/" + id + "#main-key", req, json)
        if err != nil {
            return err
        }

        _, err = client.Do(req)
        if err != nil {
            return c.String(http.StatusInternalServerError, "Internal server error")
        }
    }

    return c.String(http.StatusOK, "ok")
}

// :: Database related functions ::

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

    qb, err := x509.MarshalPKIXPublicKey(pub)
    if err != nil {
        return err
    }

    q := pem.EncodeToMemory(&pem.Block{
        Type: "PUBLIC KEY",
        Bytes: qb,
    })

    pb, err := x509.MarshalPKCS8PrivateKey(priv)
    if err != nil {
        return err
    }

    p := pem.EncodeToMemory(&pem.Block{
        Type: "PRIVATE KEY",
        Bytes: pb,
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



// PrintRequest prints the request body.
func (h Handler) PrintRequest(c echo.Context) error {

    body := c.Request().Body
    bytes, err := ioutil.ReadAll(body)
    if err != nil {
        return c.String(http.StatusInternalServerError, "Internal server error")
    }
    fmt.Println(string(bytes))

    return c.String(http.StatusOK, "ok")
}

