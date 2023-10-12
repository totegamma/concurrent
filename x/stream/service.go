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

// Service is the interface for stream service
type Service interface {
	GetRecentItems(ctx context.Context, streams []string, until time.Time, limit int) ([]core.StreamItem, error)
	GetImmediateItems(ctx context.Context, streams []string, since time.Time, limit int) ([]core.StreamItem, error)
	GetItem(ctx context.Context, stream string, id string) (core.StreamItem, error)
	PostItem(ctx context.Context, stream string, item core.StreamItem, body interface{}) error
	RemoveItem(ctx context.Context, stream string, id string)

	CreateStream(ctx context.Context, stream core.Stream) (core.Stream, error)
	GetStream(ctx context.Context, key string) (core.Stream, error)
	UpdateStream(ctx context.Context, stream core.Stream) (core.Stream, error)
	DeleteStream(ctx context.Context, streamID string) error

	ListStreamBySchema(ctx context.Context, schema string) ([]core.Stream, error)
	ListStreamByAuthor(ctx context.Context, author string) ([]core.Stream, error)
}

type service struct {
	rdb        *redis.Client
	repository Repository
	entity     entity.Service
	config     util.Config
}

// NewService creates a new service
func NewService(rdb *redis.Client, repository Repository, entity entity.Service, config util.Config) Service {
	return &service{rdb, repository, entity, config}
}

func ChunkDate(t time.Time) string {
	// chunk by 10 minutes
	//return fmt.Sprintf("%d", t.Unix()/600)
	return fmt.Sprintf("%d", (t.Unix()/600)*600)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetRecentItems returns recent message from streams
func (s *service) GetRecentItems(ctx context.Context, streams []string, until time.Time, limit int) ([]core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetRecentItems")
	defer span.End()

	var messages []core.StreamItem
	var buckets map[string][]string = make(map[string][]string)

	for _, stream := range streams {
		split := strings.Split(stream, "@")
		host := s.config.Concurrent.FQDN
		if len(split) != 2 {
			host = split[1]
		}

		buckets[host] = append(buckets[host], split[0])
	}

	for host, localstreams := range buckets {
		if host == s.config.Concurrent.FQDN {
			untilChunk := ChunkDate(until)
			items, err := s.repository.GetMultiChunk(ctx, localstreams, untilChunk)
			if err != nil {
				log.Printf("Error: %v", err)
				span.RecordError(err)
				continue
			}

			for _, chunkItems := range items {
				for _, item := range chunkItems {
					if item.CDate.After(until) {
						continue
					}
					messages = append(messages, item)
				}
			}

			/*
			for _, stream := range localstreams {
				items, err := s.repository.GetRecentItems(ctx, stream, until, limit)
				if err != nil {
					span.RecordError(err)
					continue
				}
				messages = append(messages, items...)
			}
			*/
		} else {
			// TODO: Get from remote
		}
	}

	var uniq []core.StreamItem
	m := make(map[string]bool)
	for _, elem := range messages {
		if !m[elem.ObjectID] {
			m[elem.ObjectID] = true
			uniq = append(uniq, elem)
		}
	}

	sort.Slice(uniq, func(l, r int) bool {
		return uniq[l].CDate.After(uniq[r].CDate)
	})

	chopped := uniq[:min(len(uniq), limit)]

	return chopped, nil
}

// GetImmediateItems returns immediate message from streams
func (s *service) GetImmediateItems(ctx context.Context, streams []string, since time.Time, limit int) ([]core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetImmediateItems")
	defer span.End()

	var messages []core.StreamItem
	var buckets map[string][]string = make(map[string][]string)

	for _, stream := range streams {
		split := strings.Split(stream, "@")
		host := s.config.Concurrent.FQDN
		if len(split) != 2 {
			host = split[1]
		}

		buckets[host] = append(buckets[host], split[0])
	}

	for host, localstreams := range buckets {
		if host == s.config.Concurrent.FQDN {
			for _, stream := range localstreams {
				items, err := s.repository.GetImmediateItems(ctx, stream, since, limit)
				if err != nil {
					span.RecordError(err)
					continue
				}
				messages = append(messages, items...)
			}
		} else {
			// TODO: Get from remote
		}
	}

	var uniq []core.StreamItem
	m := make(map[string]bool)
	for _, elem := range messages {
		if !m[elem.ObjectID] {
			m[elem.ObjectID] = true
			uniq = append(uniq, elem)
		}
	}

	sort.Slice(uniq, func(l, r int) bool {
		return uniq[l].CDate.Before(uniq[r].CDate)
	})

	chopped := uniq[:min(len(uniq), limit)]

	return chopped, nil
}

// Post posts events to the stream.
// If the stream is local, it will be posted to the local Redis.
// If the stream is remote, it will be posted to the remote domain's Checkpoint.
func (s *service) PostItem(ctx context.Context, stream string, item core.StreamItem, body interface{}) error {
	ctx, span := tracer.Start(ctx, "ServicePostItem")
	defer span.End()

	span.SetAttributes(attribute.String("stream", stream))

	query := strings.Split(stream, "@")
	if len(query) != 2 {
		return fmt.Errorf("Invalid format: %v", stream)
	}

	streamID, streamHost := query[0], query[1]

	item.StreamID = streamID

	if streamHost == s.config.Concurrent.FQDN {

		// check if the user has write access to the stream
		if !s.repository.HasWriteAccess(ctx, streamID, item.Author) {
			return fmt.Errorf("You don't have write access to %v", streamID)
		}

		// add to stream
		created, err := s.repository.CreateItem(ctx, item)
		if err != nil {
			span.RecordError(err)
			log.Printf("fail to create item: %v", err)
			return err
		}

		// publish event to pubsub
		jsonstr, _ := json.Marshal(Event{
			Stream: stream,
			Action: "create",
			Type:   item.Type,
			Item:   created,
			Body:   body,
		})
		err = s.rdb.Publish(context.Background(), stream, jsonstr).Err()
		if err != nil {
			span.RecordError(err)
			log.Printf("fail to publish message to Redis: %v", err)
			return err
		}
	} else {
		packet := checkpointPacket{
			Item: item,
			Body: body,
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
		}, s.config.Concurrent.PrivateKey)

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

// Create updates stream information
func (s *service) CreateStream(ctx context.Context, obj core.Stream) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "ServiceCreate")
	defer span.End()

	if obj.ID != "" {
		return core.Stream{}, fmt.Errorf("id must be empty")
	}
	obj.ID = xid.New().String()

	created, err := s.repository.CreateStream(ctx, obj)
	created.ID = created.ID + "@" + s.config.Concurrent.FQDN

	return created, err
}

// Update updates stream information
func (s *service) UpdateStream(ctx context.Context, obj core.Stream) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "ServiceUpdate")
	defer span.End()

	split := strings.Split(obj.ID, "@")
	if len(split) == 2 {
		if split[1] != s.config.Concurrent.FQDN {
			return core.Stream{}, fmt.Errorf("this stream is not managed by this domain")
		}
		obj.ID = split[0]
	}

	updated, err := s.repository.UpdateStream(ctx, obj)

	updated.ID = updated.ID + "@" + s.config.Concurrent.FQDN

	return updated, err
}

// Get returns stream information by ID
func (s *service) GetStream(ctx context.Context, key string) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	return s.repository.GetStream(ctx, key)
}

// StreamListBySchema returns streamList by schema
func (s *service) ListStreamBySchema(ctx context.Context, schema string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "ServiceStreamListBySchema")
	defer span.End()

	streams, err := s.repository.ListStreamBySchema(ctx, schema)
	for i := 0; i < len(streams); i++ {
		streams[i].ID = streams[i].ID + "@" + s.config.Concurrent.FQDN
	}
	return streams, err
}

// StreamListByAuthor returns streamList by author
func (s *service) ListStreamByAuthor(ctx context.Context, author string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "ServiceStreamListByAuthor")
	defer span.End()

	streams, err := s.repository.ListStreamByAuthor(ctx, author)
	for i := 0; i < len(streams); i++ {
		streams[i].ID = streams[i].ID + "@" + s.config.Concurrent.FQDN
	}
	return streams, err
}

// GetItem returns stream element by ID
func (s *service) GetItem(ctx context.Context, stream string, id string) (core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetItem")
	defer span.End()

	return s.repository.GetItem(ctx, stream, id)
}

// Remove removes stream element by ID
func (s *service) RemoveItem(ctx context.Context, stream string, id string) {
	ctx, span := tracer.Start(ctx, "ServiceRemoveItem")
	defer span.End()

	s.repository.DeleteItem(ctx, stream, id)
}

// Delete deletes
func (s *service) DeleteStream(ctx context.Context, streamID string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.DeleteStream(ctx, streamID)
}
