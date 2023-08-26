// Package activitypub provides an ActivityPub server.
package activitypub

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var tracer = otel.Tracer("activitypub")

// Handler is a handler for the WebFinger protocol.
type Handler struct {
	repo        *Repository
	rdb         *redis.Client
	message     *message.Service
	association *association.Service
	config      util.Config
	apconfig    APConfig
}

// NewHandler returns a new Handler.
func NewHandler(
	repo *Repository,
	rdb *redis.Client,
	message *message.Service,
	association *association.Service,
	config util.Config,
	apconfig APConfig,
) *Handler {
	return &Handler{repo, rdb, message, association, config, apconfig}
}

// :: Activitypub Related Functions ::

// WebFinger handles WebFinger requests.
func (h Handler) WebFinger(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "WebFinger")
	defer span.End()

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
	if domain != h.config.Concurrent.FQDN {
		return c.String(http.StatusBadRequest, "Invalid resource")
	}

	_, err := h.repo.GetEntityByID(ctx, username)
	if err != nil {
		return c.String(http.StatusNotFound, "entity not found")
	}

	return c.JSON(http.StatusOK, WebFinger{
		Subject: resource,
		Links: []WebFingerLink{
			{
				Rel:  "self",
				Type: "application/activity+json",
				Href: "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + username,
			},
		},
	})
}

// User handles user requests.
func (h Handler) User(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "User")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	entity, err := h.repo.GetEntityByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	person, err := h.repo.GetPersonByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "person not found")
	}

	return c.JSON(http.StatusOK, Person{
		Context:           "https://www.w3.org/ns/activitystreams",
		Type:              "Person",
		ID:                "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + id,
		Inbox:             "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + id + "/inbox",
		Outbox:            "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + id + "/outbox",
		PreferredUsername: id,
		Name:              person.Name,
		Summary:           person.Summary,
		URL:               person.ProfileURL,
		Icon: Icon{
			Type:      "Image",
			MediaType: "image/png",
			URL:       person.IconURL,
		},
		PublicKey: Key{
			ID:           "https://" + h.config.Concurrent.FQDN + "/ap/key/" + id,
			Type:         "Key",
			Owner:        "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + id,
			PublicKeyPem: entity.Publickey,
		},
	})
}

// Note handles note requests.
func (h Handler) Note(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Note")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.String(http.StatusBadRequest, "Invalid noteID")
	}
	msg, err := h.message.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "message not found")
	}

	entity, err := h.repo.GetEntityByCCID(ctx, msg.Author)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	var signedObject message.SignedObject
	err = json.Unmarshal([]byte(msg.Payload), &signedObject)
	if err != nil {
		span.RecordError(err)
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
		Context:      "https://www.w3.org/ns/activitystreams",
		Type:         "Note",
		ID:           "https://" + h.config.Concurrent.FQDN + "/ap/note/" + id,
		AttributedTo: "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + entity.ID,
		Content:      text,
		Published:    msg.CDate.Format(time.RFC3339),
		To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
	})
}

// Inbox handles inbox requests.
func (h Handler) Inbox(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerAPInbox")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	_, err := h.repo.GetEntityByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	var object Object
	err = c.Bind(&object)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Invalid request body")
	}

	switch object.Type {
	case "Follow":

		requester, err := FetchPerson(ctx, object.Actor)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusBadRequest, "Invalid request body")
		}
		accept := Accept{
			Context: "https://www.w3.org/ns/activitystreams",
			ID:      "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + id + "/follows/" + url.PathEscape(requester.ID),
			Type:    "Accept",
			Actor:   "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + id,
			Object:  object,
		}

		split := strings.Split(object.Object.(string), "/")
		userID := split[len(split)-1]

		err = h.PostToInbox(ctx, requester.Inbox, accept, userID)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error")
		}

		// check follow already exists
		_, err = h.repo.GetFollowByID(ctx, object.ID)
		if err == nil {
			return c.String(http.StatusOK, "follow already exists")
		}

		// save follow
		err = h.repo.SaveFollow(ctx, ApFollow{
			ID:              object.ID,
			SubscriberInbox: requester.Inbox,
			PublisherUserID: userID,
		})
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error (save follow error)")
		}

		return c.String(http.StatusOK, "follow accepted")

	case "Like":
		targetID := strings.Replace(object.Object.(string), "https://"+h.config.Concurrent.FQDN+"/ap/note/", "", 1)
		_, err := h.message.Get(ctx, targetID)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusNotFound, "message not found")
		}

		person, err := FetchPerson(ctx, object.Actor)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusBadRequest, "failed to fetch actor")
		}

		b := association.SignedObject{
			Signer: h.apconfig.ProxyCCID,
			Type:   "Association",
			Schema: "https://raw.githubusercontent.com/totegamma/concurrent-schemas/master/associations/emoji/0.0.1.json",
			Body: map[string]interface{}{
				"shortcode": object.Tag[0].Name,
				"imageUrl":  object.Tag[0].Icon.URL,
				"profileOverride": map[string]interface{}{
					"username":    person.Name,
					"avatar":      person.Icon.URL,
					"description": person.Summary,
					"link":        object.Actor,
				},
			},
			Meta: map[string]interface{}{
				"apActor": object.Actor,
			},
			SignedAt: time.Now(),
			Target:   targetID,
		}

		objb, err := json.Marshal(b)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error (json marshal error)")
		}

		objstr := string(objb)
		objsig, err := util.SignBytes(objb, h.apconfig.ProxyPrivateKey)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error (sign error)")
		}

		_, err = h.association.PostAssociation(ctx, objstr, objsig, []string{}, "messages")
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error (post association error)")
		}

		return c.String(http.StatusOK, "like accepted")

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
			// check follow already deleted
			_, err := h.repo.GetFollowByID(ctx, id)
			if err != nil {
				return c.String(http.StatusOK, "follow already undoed")
			}
			h.repo.RemoveFollow(ctx, id)
			return c.String(http.StatusOK, "OK")
		default:
			// print request body
			b, err := json.Marshal(object)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (json marshal error)")
			}
			log.Println(string(b))
			return c.String(http.StatusOK, "OK but not implemented")
		}
	default:
		// print request body
		b, err := json.Marshal(object)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error (json marshal error)")
		}
		log.Println(string(b))
		return c.String(http.StatusOK, "OK but not implemented")
	}

	// return c.String(http.StatusInternalServerError, "Internal server error")
}

// :: Database related functions ::

// GetPerson handles entity fetches.
func (h Handler) GetPerson(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "GetPerson")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	person, err := h.repo.GetPersonByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": person})
}

// UpdatePerson handles entity updates.
func (h Handler) UpdatePerson(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "UpdatePerson")
	defer span.End()

	claims := c.Get("jwtclaims").(util.JwtClaims)
	ccid := claims.Audience

	entity, err := h.repo.GetEntityByCCID(ctx, ccid)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	if entity.CCID != ccid {
		return c.String(http.StatusUnauthorized, "unauthorized")
	}

	var person ApPerson
	err = c.Bind(&person)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Invalid request body")
	}

	created, err := h.repo.UpsertPerson(ctx, person)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusInternalServerError, "Internal server error")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": created})
}

// CreateEntity handles entity creation.
func (h Handler) CreateEntity(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "CreateEntity")
	defer span.End()

	claims := c.Get("jwtclaims").(util.JwtClaims)
	ccid := claims.Audience

	var request CreateEntityRequest
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Invalid request body")
	}

	// check if entity already exists
	_, err = h.repo.GetEntityByCCID(ctx, ccid)
	if err == nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Entity already exists")
	}

	// create ed25519 keypair
	pub, priv, err := ed25519.GenerateKey(nil)

	qb, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		span.RecordError(err)
		return err
	}

	q := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: qb,
	})

	pb, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		span.RecordError(err)
		return err
	}

	p := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pb,
	})

	created, err := h.repo.CreateEntity(ctx, ApEntity{
		ID:         request.ID,
		CCID:       ccid,
		Publickey:  string(q),
		Privatekey: string(p),
	})
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusInternalServerError, "Internal server error")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": created})
}

// GetEntityID handles entity id requests.
func (h Handler) GetEntityID(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "GetEntityID")
	defer span.End()

	ccid := c.Param("ccid")
	if ccid == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	entity, err := h.repo.GetEntityByCCID(ctx, ccid)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity.ID})
}

func (h Handler) NodeInfoWellKnown(c echo.Context) error {
	_, span := tracer.Start(c.Request().Context(), "NodeInfoWellKnown")
	defer span.End()

	return c.JSON(http.StatusOK, WellKnown{
		Links: []WellKnownLink{
			{
				Rel:  "http://nodeinfo.diaspora.software/ns/schema/2.0",
				Href: "https://" + h.config.Concurrent.FQDN + "/ap/nodeinfo/2.0",
			},
		},
	})
}

// NodeInfo handles nodeinfo requests
func (h Handler) NodeInfo(c echo.Context) error {
	_, span := tracer.Start(c.Request().Context(), "NodeInfo")
	defer span.End()

	return c.JSON(http.StatusOK, NodeInfo{
		Version: "2.0",
		Software: NodeInfoSoftware{
			Name:    "Concurrent",
			Version: util.GetGitShortHash(),
		},
		Protocols: []string{
			"activitypub",
		},
		OpenRegistrations: h.config.Concurrent.Registration == "open",
		Metadata: NodeInfoMetadata{
			NodeName:        h.config.Profile.Nickname,
			NodeDescription: h.config.Profile.Description,
			Maintainer: NodeInfoMetadataMaintainer{
				Name:  h.config.Profile.MaintainerName,
				Email: h.config.Profile.MaintainerEmail,
			},
			ThemeColor: h.config.Profile.ThemeColor,
		},
	})
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
