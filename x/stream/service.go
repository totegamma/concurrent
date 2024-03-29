//go:generate go run go.uber.org/mock/mockgen -source=service.go -destination=mock/service.go

package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/xid"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/jwt"
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

	PublishEventToLocal(ctx context.Context, event core.Event) error
	DistributeEvent(ctx context.Context, stream string, event core.Event) error

	CreateStream(ctx context.Context, stream core.Stream) (core.Stream, error)
	GetStream(ctx context.Context, key string) (core.Stream, error)
	UpdateStream(ctx context.Context, stream core.Stream) (core.Stream, error)
	DeleteStream(ctx context.Context, streamID string) error

	HasWriteAccess(ctx context.Context, key string, author string) bool
	HasReadAccess(ctx context.Context, key string, author string) bool

	ListStreamBySchema(ctx context.Context, schema string) ([]core.Stream, error)
	ListStreamByAuthor(ctx context.Context, author string) ([]core.Stream, error)

	GetChunks(ctx context.Context, streams []string, pivot time.Time) (map[string]Chunk, error)
	GetChunksFromRemote(ctx context.Context, host string, streams []string, pivot time.Time) (map[string]Chunk, error)

	Checkpoint(ctx context.Context, stream string, item core.StreamItem, body interface{}, principal string, requesterDomain string) error

	ListStreamSubscriptions(ctx context.Context) (map[string]int64, error)
	Count(ctx context.Context) (int64, error)
}

type service struct {
	repository Repository
	entity     entity.Service
	domain     domain.Service
	config     util.Config
}

// NewService creates a new service
func NewService(repository Repository, entity entity.Service, domain domain.Service, config util.Config) Service {
	return &service{repository, entity, domain, config}
}

func (s *service) Checkpoint(ctx context.Context, stream string, item core.StreamItem, body interface{}, principal string, requesterDomain string) error {
	ctx, span := tracer.Start(ctx, "ServiceCheckpoint")
	defer span.End()

	if principal != "" { // TODO: for backward compatibility. Remove this condition after next release
		_, err := s.entity.ResolveHost(ctx, principal, requesterDomain) // 一発resolveして存在確認+なければ取得を走らせておく
		if err != nil {
			span.RecordError(err)
			return err
		}
	}

	err := s.PostItem(ctx, stream, item, body)
	return err
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceCount")
	defer span.End()

	return s.repository.Count(ctx)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *service) GetChunksFromRemote(ctx context.Context, host string, streams []string, pivot time.Time) (map[string]Chunk, error) {
	return s.repository.GetChunksFromRemote(ctx, host, streams, pivot)
}

// GetChunks returns chunks by streamID and time
func (s *service) GetChunks(ctx context.Context, streams []string, until time.Time) (map[string]Chunk, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetChunks")
	defer span.End()

	// normalize streamID and validate
	for i, stream := range streams {
		if !strings.Contains(stream, "@") {
			streams[i] = fmt.Sprintf("%s@%s", stream, s.config.Concurrent.FQDN)
		} else {
			split := strings.Split(stream, "@")
			if len(split) != 2 {
				return nil, fmt.Errorf("invalid streamID: %s", stream)
			}
			if split[1] != s.config.Concurrent.FQDN {
				return nil, fmt.Errorf("invalid streamID: %s", stream)
			}
		}
	}

	// first, try to get from cache
	untilChunk := core.Time2Chunk(until)
	items, err := s.repository.GetChunksFromCache(ctx, streams, untilChunk)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get chunks from cache", slog.String("error", err.Error()), slog.String("module", "stream"))
		span.RecordError(err)
		return nil, err
	}

	// if not found in cache, get from db
	missingStreams := make([]string, 0)
	for _, stream := range streams {
		if _, ok := items[stream]; !ok {
			missingStreams = append(missingStreams, stream)
		}
	}

	if len(missingStreams) > 0 {
		// get from db
		dbItems, err := s.repository.GetChunksFromDB(ctx, missingStreams, untilChunk)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get chunks from db", slog.String("error", err.Error()), slog.String("module", "stream"))
			span.RecordError(err)
			return nil, err
		}
		// merge
		for k, v := range dbItems {
			items[k] = v
		}
	}

	return items, nil
}

// GetRecentItems returns recent message from streams
func (s *service) GetRecentItems(ctx context.Context, streams []string, until time.Time, limit int) ([]core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetRecentItems")
	defer span.End()

	// normalize streamID and validate
	for i, stream := range streams {
		if !strings.Contains(stream, "@") {
			streams[i] = fmt.Sprintf("%s@%s", stream, s.config.Concurrent.FQDN)
		} else {
			split := strings.Split(stream, "@")
			if len(split) != 2 {
				return nil, fmt.Errorf("invalid streamID: %s", stream)
			}
		}
	}

	// first, try to get from cache regardless of local or remote
	untilChunk := core.Time2Chunk(until)
	items, err := s.repository.GetChunksFromCache(ctx, streams, untilChunk)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get chunks from cache", slog.String("error", err.Error()), slog.String("module", "stream"))
		span.RecordError(err)
		return nil, err
	}

	// if not found in cache, get from remote by host
	buckets := make(map[string][]string)
	for _, stream := range streams {
		if _, ok := items[stream]; !ok {
			split := strings.Split(stream, "@")
			if len(split) != 2 {
				continue
			}
			buckets[split[1]] = append(buckets[split[1]], split[0])
		}
	}

	for host, streams := range buckets {
		if host == s.config.Concurrent.FQDN {
			chunks, err := s.repository.GetChunksFromDB(ctx, streams, untilChunk)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get chunks from db", slog.String("error", err.Error()), slog.String("module", "stream"))
				span.RecordError(err)
				return nil, err
			}
			for stream, chunk := range chunks {
				items[stream] = chunk
			}
		} else {
			chunks, err := s.repository.GetChunksFromRemote(ctx, host, streams, until)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get chunks from remote", slog.String("error", err.Error()), slog.String("module", "stream"))
				span.RecordError(err)
				continue
			}
			for stream, chunk := range chunks {
				items[stream] = chunk
			}
		}
	}

	// summary messages and remove earlier than until
	var messages []core.StreamItem
	for _, item := range items {
		for _, streamItem := range item.Items {
			if streamItem.CDate.After(until) {
				continue
			}
			messages = append(messages, streamItem)
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

	author := item.Author
	if author == "" {
		author = item.Owner
	}

	if streamHost == s.config.Concurrent.FQDN {

		// check if the user has write access to the stream
		if !s.HasWriteAccess(ctx, streamID, author) {
			slog.InfoContext(
				ctx, "failed to post to stream",
				slog.String("type", "audit"),
				slog.String("principal", author),
				slog.String("stream", streamID),
				slog.String("module", "stream"),
			)
			return fmt.Errorf("You don't have write access to %v", streamID)
		}

		slog.DebugContext(
			ctx, fmt.Sprintf("post to local stream: %v to %v", item.ObjectID, streamID),
			slog.String("module", "stream"),
		)

		// add to stream
		created, err := s.repository.CreateItem(ctx, item)
		if err != nil {
			slog.ErrorContext(ctx, "failed to create item", slog.String("error", err.Error()), slog.String("module", "stream"))
			span.RecordError(err)
			return err
		}

		// publish event to pubsub
		event := core.Event{
			Stream: stream,
			Action: "create",
			Type:   item.Type,
			Item:   created,
			Body:   body,
		}

		err = s.repository.PublishEvent(ctx, event)
		if err != nil {
			slog.ErrorContext(ctx, "failed to publish event", slog.String("error", err.Error()), slog.String("module", "stream"))
			span.RecordError(err)
			return err
		}
	} else {

		slog.DebugContext(
			ctx, fmt.Sprintf("post to remote stream: %v to %v@%v", item.ObjectID, streamID, streamHost),
			slog.String("module", "stream"),
		)

		// check if domain exists
		_, err := s.domain.GetByFQDN(ctx, streamHost)
		if err != nil {
			// Hook to say hello to the remote domain
			_, err = s.domain.SayHello(ctx, streamHost)
			if err != nil {
				span.RecordError(err)
				return err
			}
		}

		packet := checkpointPacket{
			Stream:    stream,
			Item:      item,
			Body:      body,
			Principal: author,
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

		jwt, err := jwt.Create(jwt.Claims{
			Issuer:         s.config.Concurrent.CCID,
			Subject:        "CC_API",
			Audience:       streamHost,
			ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
			IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
			JWTID:          xid.New().String(),
		}, s.config.Concurrent.PrivateKey)

		req.Header.Add("content-type", "application/json")
		req.Header.Add("authorization", "Bearer "+jwt)
		client := new(http.Client)
		client.Timeout = 10 * time.Second
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

func (s *service) PublishEventToLocal(ctx context.Context, event core.Event) error {
	ctx, span := tracer.Start(ctx, "ServiceDistributeEvents")
	defer span.End()

	return s.repository.PublishEvent(ctx, event)
}

// DistributeEvent distributes events to the stream.
func (s *service) DistributeEvent(ctx context.Context, stream string, event core.Event) error {
	ctx, span := tracer.Start(ctx, "ServiceDistributeEvents")
	defer span.End()

	query := strings.Split(stream, "@")
	if len(query) != 2 {
		return fmt.Errorf("Invalid format: %v", stream)
	}

	_, streamHost := query[0], query[1]

	if streamHost == s.config.Concurrent.FQDN {

		s.repository.PublishEvent(ctx, event)

	} else {

		jsonstr, _ := json.Marshal(event)

		req, err := http.NewRequest(
			"POST",
			"https://"+streamHost+"/api/v1/streams/checkpoint/event",
			bytes.NewBuffer(jsonstr),
		)

		if err != nil {
			span.RecordError(err)
			return err
		}

		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

		jwt, err := jwt.Create(jwt.Claims{
			Issuer:         s.config.Concurrent.CCID,
			Subject:        "CC_API",
			Audience:       streamHost,
			ExpirationTime: strconv.FormatInt(time.Now().Add(1*time.Minute).Unix(), 10),
			IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
			JWTID:          xid.New().String(),
		}, s.config.Concurrent.PrivateKey)

		req.Header.Add("content-type", "application/json")
		req.Header.Add("authorization", "Bearer "+jwt)
		client := new(http.Client)
		client.Timeout = 10 * time.Second
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

func (s *service) ListStreamSubscriptions(ctx context.Context) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceListStreamSubscriptions")
	defer span.End()

	return s.repository.ListStreamSubscriptions(ctx)
}

func (s *service) getStreamAutoDomain(ctx context.Context, streamID string) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetStreamAutoDomain")
	defer span.End()

	key := streamID
	host := s.config.Concurrent.FQDN

	split := strings.Split(streamID, "@")
	if len(split) > 1 {
		key = split[0]
		host = split[1]
	}

	if host == s.config.Concurrent.FQDN {
		return s.repository.GetStream(ctx, key)
	} else {
		return s.repository.GetStreamFromRemote(ctx, host, key)
	}
}

func (s *service) HasWriteAccess(ctx context.Context, streamID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "ServiceHasWriteAccess")
	defer span.End()

	stream, err := s.getStreamAutoDomain(ctx, streamID)
	if err != nil {
		return false
	}

	if stream.Author == userAddress {
		return true
	}

	if len(stream.Writer) == 0 {
		return true
	}

	return slices.Contains(stream.Writer, userAddress)
}

func (s *service) HasReadAccess(ctx context.Context, streamID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "ServiceHasReadAccess")
	defer span.End()

	span.SetAttributes(attribute.String("stream", streamID))
	span.SetAttributes(attribute.String("user", userAddress))

	stream, err := s.getStreamAutoDomain(ctx, streamID)
	if err != nil {
		span.AddEvent("stream not found")
		return false
	}

	span.SetAttributes(attribute.StringSlice("reader", stream.Reader))

	if stream.Author == userAddress {
		span.AddEvent("author has read access")
		return true
	}

	if len(stream.Reader) == 0 {
		span.AddEvent("no reader")
		return true
	}

	return slices.Contains(stream.Reader, userAddress)
}
