//go:generate go run go.uber.org/mock/mockgen -source=service.go -destination=mock/service.go

package timeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/xid"
	"github.com/totegamma/concurrent/x/cdid"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/semanticid"
	"github.com/totegamma/concurrent/x/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Service is the interface for timeline service
type Service interface {
	GetRecentItems(ctx context.Context, timelines []string, until time.Time, limit int) ([]core.TimelineItem, error)
	GetImmediateItems(ctx context.Context, timelines []string, since time.Time, limit int) ([]core.TimelineItem, error)
	GetItem(ctx context.Context, timeline string, id string) (core.TimelineItem, error)
	PostItem(ctx context.Context, timeline string, item core.TimelineItem, body interface{}) error
	RemoveItem(ctx context.Context, timeline string, id string)

	PublishEventToLocal(ctx context.Context, event core.Event) error
	DistributeEvent(ctx context.Context, timeline string, event core.Event) error

	UpsertTimeline(ctx context.Context, document, signature string) (core.Timeline, error)
	GetTimeline(ctx context.Context, key string) (core.Timeline, error)
	DeleteTimeline(ctx context.Context, timelineID string) error

	HasWriteAccess(ctx context.Context, key string, author string) bool
	HasReadAccess(ctx context.Context, key string, author string) bool

	ListTimelineBySchema(ctx context.Context, schema string) ([]core.Timeline, error)
	ListTimelineByAuthor(ctx context.Context, author string) ([]core.Timeline, error)

	GetChunks(ctx context.Context, timelines []string, pivot time.Time) (map[string]Chunk, error)
	GetChunksFromRemote(ctx context.Context, host string, timelines []string, pivot time.Time) (map[string]Chunk, error)

	Checkpoint(ctx context.Context, timeline string, item core.TimelineItem, body interface{}, principal string, requesterDomain string) error

	ListTimelineSubscriptions(ctx context.Context) (map[string]int64, error)
	Count(ctx context.Context) (int64, error)
}

type service struct {
	repository Repository
	entity     entity.Service
	domain     domain.Service
	semanticid semanticid.Service
	config     util.Config
}

// NewService creates a new service
func NewService(repository Repository, entity entity.Service, domain domain.Service, semanticid semanticid.Service, config util.Config) Service {
	return &service{repository, entity, domain, semanticid, config}
}

func (s *service) Checkpoint(ctx context.Context, timeline string, item core.TimelineItem, body interface{}, principal string, requesterDomain string) error {
	ctx, span := tracer.Start(ctx, "ServiceCheckpoint")
	defer span.End()

	_, err := s.entity.ResolveHost(ctx, principal, requesterDomain) // 一発resolveして存在確認+なければ取得を走らせておく
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = s.PostItem(ctx, timeline, item, body)
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

func (s *service) GetChunksFromRemote(ctx context.Context, host string, timelines []string, pivot time.Time) (map[string]Chunk, error) {
	return s.repository.GetChunksFromRemote(ctx, host, timelines, pivot)
}

// GetChunks returns chunks by timelineID and time
func (s *service) GetChunks(ctx context.Context, timelines []string, until time.Time) (map[string]Chunk, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetChunks")
	defer span.End()

	// normalize timelineID and validate
	for i, timeline := range timelines {
		if !strings.Contains(timeline, "@") {
			timelines[i] = fmt.Sprintf("%s@%s", timeline, s.config.Concurrent.FQDN)
		} else {
			split := strings.Split(timeline, "@")
			if len(split) != 2 {
				return nil, fmt.Errorf("invalid timelineID: %s", timeline)
			}
			if split[1] != s.config.Concurrent.FQDN {
				return nil, fmt.Errorf("invalid timelineID: %s", timeline)
			}
		}
	}

	// first, try to get from cache
	untilChunk := core.Time2Chunk(until)
	items, err := s.repository.GetChunksFromCache(ctx, timelines, untilChunk)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get chunks from cache", slog.String("error", err.Error()), slog.String("module", "timeline"))
		span.RecordError(err)
		return nil, err
	}

	// if not found in cache, get from db
	missingTimelines := make([]string, 0)
	for _, timeline := range timelines {
		if _, ok := items[timeline]; !ok {
			missingTimelines = append(missingTimelines, timeline)
		}
	}

	if len(missingTimelines) > 0 {
		// get from db
		dbItems, err := s.repository.GetChunksFromDB(ctx, missingTimelines, untilChunk)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get chunks from db", slog.String("error", err.Error()), slog.String("module", "timeline"))
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

// GetRecentItems returns recent message from timelines
func (s *service) GetRecentItems(ctx context.Context, timelines []string, until time.Time, limit int) ([]core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetRecentItems")
	defer span.End()

	// normalize timelineID and validate
	for i, timeline := range timelines {
		if !strings.Contains(timeline, "@") {
			timelines[i] = fmt.Sprintf("%s@%s", timeline, s.config.Concurrent.FQDN)
		} else {
			split := strings.Split(timeline, "@")
			if len(split) != 2 {
				return nil, fmt.Errorf("invalid timelineID: %s", timeline)
			}
		}
	}

	// first, try to get from cache regardless of local or remote
	untilChunk := core.Time2Chunk(until)
	items, err := s.repository.GetChunksFromCache(ctx, timelines, untilChunk)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get chunks from cache", slog.String("error", err.Error()), slog.String("module", "timeline"))
		span.RecordError(err)
		return nil, err
	}

	// if not found in cache, get from remote by host
	buckets := make(map[string][]string)
	for _, timeline := range timelines {
		if _, ok := items[timeline]; !ok {
			split := strings.Split(timeline, "@")
			if len(split) != 2 {
				continue
			}
			buckets[split[1]] = append(buckets[split[1]], split[0])
		}
	}

	for host, timelines := range buckets {
		if host == s.config.Concurrent.FQDN {
			chunks, err := s.repository.GetChunksFromDB(ctx, timelines, untilChunk)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get chunks from db", slog.String("error", err.Error()), slog.String("module", "timeline"))
				span.RecordError(err)
				return nil, err
			}
			for timeline, chunk := range chunks {
				items[timeline] = chunk
			}
		} else {
			chunks, err := s.repository.GetChunksFromRemote(ctx, host, timelines, until)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get chunks from remote", slog.String("error", err.Error()), slog.String("module", "timeline"))
				span.RecordError(err)
				continue
			}
			for timeline, chunk := range chunks {
				items[timeline] = chunk
			}
		}
	}

	// summary messages and remove earlier than until
	var messages []core.TimelineItem
	for _, item := range items {
		for _, timelineItem := range item.Items {
			if timelineItem.CDate.After(until) {
				continue
			}
			messages = append(messages, timelineItem)
		}
	}

	var uniq []core.TimelineItem
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

// GetImmediateItems returns immediate message from timelines
func (s *service) GetImmediateItems(ctx context.Context, timelines []string, since time.Time, limit int) ([]core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetImmediateItems")
	defer span.End()

	var messages []core.TimelineItem
	var buckets map[string][]string = make(map[string][]string)

	for _, timeline := range timelines {
		split := strings.Split(timeline, "@")
		host := s.config.Concurrent.FQDN
		if len(split) != 2 {
			host = split[1]
		}

		buckets[host] = append(buckets[host], split[0])
	}

	for host, localtimelines := range buckets {
		if host == s.config.Concurrent.FQDN {
			for _, timeline := range localtimelines {
				items, err := s.repository.GetImmediateItems(ctx, timeline, since, limit)
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

	var uniq []core.TimelineItem
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

// Post posts events to the timeline.
// If the timeline is local, it will be posted to the local Redis.
// If the timeline is remote, it will be posted to the remote domain's Checkpoint.
func (s *service) PostItem(ctx context.Context, timeline string, item core.TimelineItem, body interface{}) error {
	ctx, span := tracer.Start(ctx, "ServicePostItem")
	defer span.End()

	span.SetAttributes(attribute.String("timeline", timeline))

	query := strings.Split(timeline, "@")
	if len(query) != 2 {
		return fmt.Errorf("Invalid format: %v", timeline)
	}

	timelineID, timelineHost := query[0], query[1]

	item.TimelineID = timelineID

	author := item.Owner
	if item.Author != nil {
		author = *item.Author
	}

	if timelineHost == s.config.Concurrent.FQDN {

		// check if the user has write access to the timeline
		if !s.HasWriteAccess(ctx, timelineID, author) {
			slog.InfoContext(
				ctx, "failed to post to timeline",
				slog.String("type", "audit"),
				slog.String("principal", author),
				slog.String("timeline", timelineID),
				slog.String("module", "timeline"),
			)
			return fmt.Errorf("You don't have write access to %v", timelineID)
		}

		slog.DebugContext(
			ctx, fmt.Sprintf("post to local timeline: %v to %v", item.ObjectID, timelineID),
			slog.String("module", "timeline"),
		)

		// add to timeline
		created, err := s.repository.CreateItem(ctx, item)
		if err != nil {
			slog.ErrorContext(ctx, "failed to create item", slog.String("error", err.Error()), slog.String("module", "timeline"))
			span.RecordError(err)
			return err
		}

		typ := core.TypedIDToType(created.ObjectID)

		// publish event to pubsub
		event := core.Event{
			TimelineID: timeline,
			Action:     "create",
			Type:       typ,
			Item:       created,
			Body:       body,
		}

		err = s.repository.PublishEvent(ctx, event)
		if err != nil {
			slog.ErrorContext(ctx, "failed to publish event", slog.String("error", err.Error()), slog.String("module", "timeline"))
			span.RecordError(err)
			return err
		}
	} else {

		slog.DebugContext(
			ctx, fmt.Sprintf("post to remote timeline: %v to %v@%v", item.ObjectID, timelineID, timelineHost),
			slog.String("module", "timeline"),
		)

		// check if domain exists
		_, err := s.domain.GetByFQDN(ctx, timelineHost)
		if err != nil { // TODO
			span.RecordError(err)
			return err
		}

		packet := checkpointPacket{
			Timeline:  timeline,
			Item:      item,
			Body:      body,
			Principal: author,
		}
		packetStr, err := json.Marshal(packet)
		if err != nil {
			span.RecordError(err)
			return err
		}
		req, err := http.NewRequest("POST", "https://"+timelineHost+"/api/v1/timelines/checkpoint", bytes.NewBuffer(packetStr))

		if err != nil {
			span.RecordError(err)
			return err
		}

		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

		jwt, err := jwt.Create(jwt.Claims{
			Issuer:         s.config.Concurrent.CCID,
			Subject:        "CC_API",
			Audience:       timelineHost,
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

// DistributeEvent distributes events to the timeline.
func (s *service) DistributeEvent(ctx context.Context, timeline string, event core.Event) error {
	ctx, span := tracer.Start(ctx, "ServiceDistributeEvents")
	defer span.End()

	query := strings.Split(timeline, "@")
	if len(query) != 2 {
		return fmt.Errorf("Invalid format: %v", timeline)
	}

	_, timelineHost := query[0], query[1]

	if timelineHost == s.config.Concurrent.FQDN {

		s.repository.PublishEvent(ctx, event)

	} else {

		jsonstr, _ := json.Marshal(event)

		req, err := http.NewRequest(
			"POST",
			"https://"+timelineHost+"/api/v1/timelines/checkpoint/event",
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
			Audience:       timelineHost,
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

// Create updates timeline information
func (s *service) UpsertTimeline(ctx context.Context, document, signature string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.UpsertTimline")
	defer span.End()

	var doc core.TimelineDocument[any]
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		return core.Timeline{}, err
	}

	// return existing timeline if semanticID exists
	if doc.SemanticID != "" {
		existingID, err := s.semanticid.Lookup(ctx, doc.SemanticID, doc.Signer)
		if err == nil { // なければなにもしない
			_, err := s.repository.GetTimeline(ctx, existingID) // 実在性チェック
			if err != nil {                                     // 実在しなければ掃除しておく
				s.semanticid.Delete(ctx, doc.SemanticID, doc.Signer)
			} else {
				if doc.ID == "" { // あるかつIDがない場合はセット
					doc.ID = existingID
				} else {
					if doc.ID != existingID { // あるかつIDが違う場合はエラー
						return core.Timeline{}, fmt.Errorf("SemanticID Mismatch: %s != %s", doc.ID, existingID)
					}
				}
			}
		}
	}

	if doc.ID == "" {
		hash := util.GetHash([]byte(document))
		hash10 := [10]byte{}
		copy(hash10[:], hash[:10])
		signedAt := doc.SignedAt
		doc.ID = cdid.New(hash10, signedAt).String()
	} else {
		split := strings.Split(doc.ID, "@")
		if len(split) == 2 {
			if split[1] != s.config.Concurrent.FQDN {
				return core.Timeline{}, fmt.Errorf("This timeline is not owned by this domain")
			}
			doc.ID = split[0]
		}
	}

	saved, err := s.repository.UpsertTimeline(ctx, core.Timeline{
		ID:          doc.ID,
		Indexable:   doc.Indexable,
		Author:      doc.Signer,
		DomainOwned: doc.DomainOwned,
		Schema:      doc.Schema,
		Document:    document,
		Signature:   signature,
	})

	if err != nil {
		return core.Timeline{}, err
	}

	if doc.SemanticID != "" {
		_, err = s.semanticid.Name(ctx, doc.SemanticID, doc.Signer, doc.ID, document, signature)
		if err != nil {
			return core.Timeline{}, err
		}
	}

	saved.ID = saved.ID + "@" + s.config.Concurrent.FQDN

	return saved, err
}

// Get returns timeline information by ID
func (s *service) GetTimeline(ctx context.Context, key string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "ServiceGet")
	defer span.End()

	split := strings.Split(key, "@")
	if len(split) == 2 {
		if split[1] == s.config.Concurrent.FQDN {
			return s.repository.GetTimeline(ctx, split[0])
		} else {
			if cdid.IsSeemsCDID(split[0], 't') {
				timeline, err := s.repository.GetTimeline(ctx, split[0])
				if err == nil {
					return timeline, nil
				}
			}
			targetID, err := s.semanticid.Lookup(ctx, split[0], split[1])
			if err != nil {
				return core.Timeline{}, err
			}
			return s.repository.GetTimeline(ctx, targetID)
		}
	} else {
		return s.repository.GetTimeline(ctx, key)
	}
}

// TimelineListBySchema returns timelineList by schema
func (s *service) ListTimelineBySchema(ctx context.Context, schema string) ([]core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "ServiceTimelineListBySchema")
	defer span.End()

	timelines, err := s.repository.ListTimelineBySchema(ctx, schema)
	for i := 0; i < len(timelines); i++ {
		timelines[i].ID = timelines[i].ID + "@" + s.config.Concurrent.FQDN
	}
	return timelines, err
}

// TimelineListByAuthor returns timelineList by author
func (s *service) ListTimelineByAuthor(ctx context.Context, author string) ([]core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "ServiceTimelineListByAuthor")
	defer span.End()

	timelines, err := s.repository.ListTimelineByAuthor(ctx, author)
	for i := 0; i < len(timelines); i++ {
		timelines[i].ID = timelines[i].ID + "@" + s.config.Concurrent.FQDN
	}
	return timelines, err
}

// GetItem returns timeline element by ID
func (s *service) GetItem(ctx context.Context, timeline string, id string) (core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetItem")
	defer span.End()

	return s.repository.GetItem(ctx, timeline, id)
}

// Remove removes timeline element by ID
func (s *service) RemoveItem(ctx context.Context, timeline string, id string) {
	ctx, span := tracer.Start(ctx, "ServiceRemoveItem")
	defer span.End()

	s.repository.DeleteItem(ctx, timeline, id)
}

// Delete deletes
func (s *service) DeleteTimeline(ctx context.Context, timelineID string) error {
	ctx, span := tracer.Start(ctx, "ServiceDelete")
	defer span.End()

	return s.repository.DeleteTimeline(ctx, timelineID)
}

func (s *service) ListTimelineSubscriptions(ctx context.Context) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "ServiceListTimelineSubscriptions")
	defer span.End()

	return s.repository.ListTimelineSubscriptions(ctx)
}

func (s *service) getTimelineAutoDomain(ctx context.Context, timelineID string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "ServiceGetTimelineAutoDomain")
	defer span.End()

	key := timelineID
	host := s.config.Concurrent.FQDN

	split := strings.Split(timelineID, "@")
	if len(split) > 1 {
		key = split[0]
		host = split[1]
	}

	if host == s.config.Concurrent.FQDN {
		return s.repository.GetTimeline(ctx, key)
	} else {
		return s.repository.GetTimelineFromRemote(ctx, host, key)
	}
}

func (s *service) HasWriteAccess(ctx context.Context, timelineID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "ServiceHasWriteAccess")
	defer span.End()

	return true

	/*
		timeline, err := s.getTimelineAutoDomain(ctx, timelineID)
		if err != nil {
			return false
		}

		if timeline.Author == userAddress {
			return true
		}

		if len(timeline.Writer) == 0 {
			return true
		}

		return slices.Contains(timeline.Writer, userAddress)
	*/
}

func (s *service) HasReadAccess(ctx context.Context, timelineID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "ServiceHasReadAccess")
	defer span.End()

	return true

	/*
		span.SetAttributes(attribute.String("timeline", timelineID))
		span.SetAttributes(attribute.String("user", userAddress))

		timeline, err := s.getTimelineAutoDomain(ctx, timelineID)
		if err != nil {
			span.AddEvent("timeline not found")
			return false
		}

		span.SetAttributes(attribute.StringSlice("reader", timeline.Reader))

		if timeline.Author == userAddress {
			span.AddEvent("author has read access")
			return true
		}

		if len(timeline.Reader) == 0 {
			span.AddEvent("no reader")
			return true
		}

		return slices.Contains(timeline.Reader, userAddress)
	*/
}
