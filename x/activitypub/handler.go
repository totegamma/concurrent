// Package activitypub provides an ActivityPub server.
package activitypub

import (
    "log"
    "fmt"
    "time"
    "strings"
    "net/url"
    "net/http"
    "io/ioutil"
    "crypto/x509"
    "encoding/pem"
    "encoding/json"
    "crypto/ed25519"
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
    msg, err := h.message.Get(id)
    if err != nil {
        return c.String(http.StatusNotFound, "message not found")
    }

    entity, err := h.repo.GetEntityByCCAddr(msg.Author)
    if err != nil {
        return c.String(http.StatusNotFound, "entity not found")
    }

    var signedObject message.SignedObject
    err = json.Unmarshal([]byte(msg.Payload), &signedObject)
    if err != nil {
        return c.String(http.StatusInternalServerError, "Internal server error(payload parse error)")
    }

    body := signedObject.Body

    var text string
    if signedObject.Schema == "https://raw.githubusercontent.com/totegamma/concurrent-schemas/master/messages/note/0.0.1.json" {
        t, ok := body.(map[string]interface{})["body"].(string)
        if !ok {
            return c.String(http.StatusInternalServerError, "Internal server error (body parse error)")
        }
        text = t
    } else {
        return c.String(http.StatusNotImplemented, "target message is not implemented for activitypub")
    }

    return c.JSON(http.StatusOK, Note{
        Context: "https://www.w3.org/ns/activitystreams",
        Type: "Note",
        ID: "https://" + h.config.FQDN + "/ap/note/" + id,
        AttributedTo: "https://" + h.config.FQDN + "/ap/acct/" + entity.ID,
        Content: text,
        Published: msg.CDate.Format(time.RFC3339),
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

    switch object.Type {
        case "Follow":
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

            split := strings.Split(object.Object.(string), "/")
            userID := split[len(split)-1]

            err = h.PostToInbox(requester.Inbox, accept, userID)
            if err != nil {
                return c.String(http.StatusInternalServerError, "Internal server error")
            }

            // save follow
            err = h.repo.SaveFollow(ApFollow{
                ID: object.ID,
                SubscriberInbox: requester.Inbox,
                PublisherUserID: userID,
            })
            if err != nil {
                return c.String(http.StatusInternalServerError, "Internal server error (save follow error)")
            }
        case "Undo":
            undoObject, ok := object.Object.(map[string]interface{})
            if !ok {
                log.Println("Invalid undo object", object.Object)
                return c.String(http.StatusBadRequest, "Invalid request body")
            }
            undoType, ok := undoObject["type"].(string)
            if !ok {
                log.Println("Invalid undo object", object.Object)
                return c.String(http.StatusBadRequest, "Invalid request body")
            }
            switch undoType {
                case "Follow":
                    id, ok := undoObject["id"].(string)
                    if !ok {
                        log.Println("Invalid undo object", object.Object)
                        return c.String(http.StatusBadRequest, "Invalid request body")
                    }
                    h.repo.RemoveFollow(id)
                    return c.String(http.StatusOK, "OK")
                default:
                    return c.String(http.StatusNotImplemented, "Not implemented")
            }
        default:
            return c.String(http.StatusNotImplemented, "Not implemented")
    }

    return c.String(http.StatusInternalServerError, "Internal server error")
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

