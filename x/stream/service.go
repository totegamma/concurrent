package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/xid"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Service is stream service
type Service struct {
	rdb        *redis.Client
	repository *Repository
	entity     *entity.Service
	config     util.Config
}

// NewService is for wire.go
func NewService(rdb *redis.Client, repository *Repository, entity *entity.Service, config util.Config) *Service {
	return &Service{rdb, repository, entity, config}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetRecent returns recent message from streams
func (s *Service) GetRecent(ctx context.Context, streams []string, limit int) ([]Element, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetRecent")
	defer span.End()

	var messages []redis.XMessage
	for _, stream := range streams {
		cmd := s.rdb.XRevRangeN(ctx, stream, "+", "-", int64(limit))
		messages = append(messages, cmd.Val()...)
	}
	m := make(map[string]bool)
	uniq := []redis.XMessage{}
	for _, elem := range messages {
		if !m[elem.Values["id"].(string)] {
			m[elem.Values["id"].(string)] = true
			uniq = append(uniq, elem)
		}
	}
	sort.Slice(uniq, func(l, r int) bool {
		lStr := strings.Replace(uniq[l].ID, "-", ".", 1)
		rStr := strings.Replace(uniq[r].ID, "-", ".", 1)
		lTime, _ := strconv.ParseFloat(lStr, 32)
		rTime, _ := strconv.ParseFloat(rStr, 32)
		return lTime > rTime
	})
	chopped := uniq[:min(len(uniq), limit)]
	result := []Element{}

	for _, elem := range chopped {
		host, _ := s.entity.ResolveHost(ctx, elem.Values["author"].(string))
		id, ok := elem.Values["id"].(string)
		if !ok {
			id = ""
		}
		typ, ok := elem.Values["type"].(string)
		if !ok {
			typ = "message"
		}
		author, ok := elem.Values["author"].(string)
		if !ok {
			author = ""
		}
		result = append(result, Element{
			Timestamp: elem.ID,
			ID:        id,
			Type:      typ,
			Author:    author,
			Domain:    host,
		})
	}

	return result, nil
}

// GetRange returns specified range messages from streams
func (s *Service) GetRange(ctx context.Context, streams []string, since string, until string, limit int) ([]Element, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetRange")
	defer span.End()

	var messages []redis.XMessage
	for _, stream := range streams {
		cmd := s.rdb.XRevRangeN(ctx, stream, until, since, int64(limit))
		messages = append(messages, cmd.Val()...)
	}
	m := make(map[string]bool)
	uniq := []redis.XMessage{}
	for _, elem := range messages {
		if !m[elem.Values["id"].(string)] {
			m[elem.Values["id"].(string)] = true
			uniq = append(uniq, elem)
		}
	}
	sort.Slice(uniq, func(l, r int) bool {
		lStr := strings.Replace(uniq[l].ID, "-", ".", 1)
		rStr := strings.Replace(uniq[r].ID, "-", ".", 1)
		lTime, _ := strconv.ParseFloat(lStr, 32)
		rTime, _ := strconv.ParseFloat(rStr, 32)
		return lTime > rTime
	})
	chopped := uniq[:min(len(uniq), limit)]
	result := []Element{}

	for _, elem := range chopped {
		host, _ := s.entity.ResolveHost(ctx, elem.Values["author"].(string))
		id, ok := elem.Values["id"].(string)
		if !ok {
			id = ""
		}
		typ, ok := elem.Values["type"].(string)
		if !ok {
			typ = "message"
		}
		author, ok := elem.Values["author"].(string)
		if !ok {
			author = ""
		}
		result = append(result, Element{
			Timestamp: elem.ID,
			ID:        id,
			Type:      typ,
			Author:    author,
			Domain:    host,
		})
	}

	return result, nil
}

// Post posts to stream
func (s *Service) Post(ctx context.Context, stream string, id string, typ string, author string, host string, owner string) error {
	ctx, span := tracer.Start(ctx, "ServicePost")
	defer span.End()

	span.SetAttributes(attribute.String("stream", stream))

	query := strings.Split(stream, "@")
	if len(query) != 2 {
		return fmt.Errorf("Invalid format: %v", stream)
	}

	if host == "" {
		host = s.config.Concurrent.FQDN
	}

	streamID, streamHost := query[0], query[1]

	if owner == "" {
		owner = author
	}

	if streamHost == s.config.Concurrent.FQDN {

		// check if the user has write access to the stream
		if !s.repository.HasWriteAccess(ctx, streamID, author) {
			return fmt.Errorf("You don't have write access to %v", streamID)
		}

		// add to stream
		timestamp, err := s.rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: streamID,
			ID:     "*",
			Values: map[string]interface{}{
				"id":     id,
				"type":   typ,
				"author": owner,
			},
		}).Result()
		if err != nil {
			span.RecordError(err)
			log.Printf("fail to xadd: %v", err)
		}

		// publish event to pubsub
		jsonstr, _ := json.Marshal(Event{
			Stream: stream,
			Type:   typ,
			Action: "create",
			Body: Element{
				Timestamp: timestamp,
				ID:        id,
				Type:      typ,
				Author:    author,
				Owner:     owner,
				Domain:    host,
			},
		})
		err = s.rdb.Publish(context.Background(), stream, jsonstr).Err()
		if err != nil {
			span.RecordError(err)
			log.Printf("fail to publish message to Redis: %v", err)
		}
	} else {
		packet := checkpointPacket{
			Stream: stream,
			ID:     id,
			Type:   typ,
			Author: author,
			Host:   s.config.Concurrent.FQDN,
			Owner:  owner,
		}
		packetStr, err := json.Marshal(packet)
		if err != nil {
			span.RecordError(err)
			return err
		}
		req, err := http.NewRequest("POST", "https://"+streamHost+"/api/v1/streams/checkpoint", bytes.NewBuffer(packetStr))

		if err != nil {
			span.RecordError(err)
			return err
		}

		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

		jwt, err := util.CreateJWT(util.JwtClaims{
			Issuer:         s.config.Concurrent.CCID,
			Subject:        "CONCURRENT_API",
			Audience:       streamHost,
			ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
			IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
			JWTID:          xid.New().String(),
		}, s.config.Concurrent.Prvkey)

		req.Header.Add("content-type", "application/json")
		req.Header.Add("authorization", "Bearer "+jwt)
		client := new(http.Client)
		resp, err := client.Do(req)
		if err != nil {
			span.RecordError(err)
			return err
		}
		defer resp.Body.Close()

		// TODO: response check
		span.AddEvent("checkpoint response", trace.WithAttributes(attribute.String("response", resp.Status)))

	}
	return nil
}

// Upsert updates stream information
func (s *Service) Upsert(ctx context.Context, objectStr string, signature string, id string) (string, error) {
	ctx, span := tracer.Start(ctx, "ServiceUpsert")
	defer span.End()

	var object signedObject
	err := json.Unmarshal([]byte(objectStr), &object)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	if err := util.VerifySignature(objectStr, object.Signer, signature); err != nil {
		span.RecordError(err)
		return "", err
	}

	if id == "" {
		id = xid.New().String()
	} else {
		split := strings.Split(id, "@")
		if len(split) != 2 {
			return "", fmt.Errorf("invalid id")
		}
		if split[1] != s.config.Concurrent.FQDN {
			return "", fmt.Errorf("invalid stream host")
		}
		id = split[0]
	}

	stream := core.Stream{
		ID:         id,
		Author:     object.Signer,
		Maintainer: object.Maintainer,
		Writer:     object.Writer,
		Reader:     object.Reader,
		Schema:     object.Schema,
		Payload:    objectStr,
		Signature:  signature,
	}

	s.repository.Upsert(ctx, &stream)
	return stream.ID + "@" + s.config.Concurrent.FQDN, nil
}

// Get returns stream information by ID
func (s *Service) Get(ctx context.Context, key string) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repository.Get(ctx, key)
}

// StreamListBySchema returns streamList by schema
func (s *Service) StreamListBySchema(ctx context.Context, schema string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "ServiceStreamListBySchema")
	defer span.End()

	streams, err := s.repository.GetList(ctx, schema)
	for i := 0; i < len(streams); i++ {
		streams[i].ID = streams[i].ID + "@" + s.config.Concurrent.FQDN
	}
	return streams, err
}

// GetElement returns stream element by ID
func (s *Service) GetElement(ctx context.Context, stream string, id string) (Element, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetElement")
	defer span.End()

	result, err := s.rdb.XRange(ctx, stream, id, id).Result()
	if err != nil {
		span.RecordError(err)
		return Element{}, err
	}
	if len(result) == 0 {
		return Element{}, fmt.Errorf("element not found")
	}
	return Element{
		Timestamp: result[0].ID,
		ID:        result[0].Values["id"].(string),
		Type:      result[0].Values["type"].(string),
		Author:    result[0].Values["author"].(string),
	}, nil
}

// Remove removes stream element by ID
func (s *Service) Remove(ctx context.Context, stream string, id string) {
	ctx, span := tracer.Start(ctx, "ServiceRemove")
	defer span.End()

	s.rdb.XDel(ctx, stream, id)
}

// Delete deletes
func (s *Service) Delete(ctx context.Context, streamID string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.Delete(ctx, streamID)
}
