package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"

	"github.com/totegamma/concurrent/cdid"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
)

type service struct {
	repository   Repository
	entity       core.EntityService
	domain       core.DomainService
	semanticid   core.SemanticIDService
	subscription core.SubscriptionService
	config       util.Config
}

// NewService creates a new service
func NewService(
	repository Repository,
	entity core.EntityService,
	domain core.DomainService,
	semanticid core.SemanticIDService,
	subscription core.SubscriptionService,
	config util.Config,
) core.TimelineService {
	return &service{
		repository,
		entity,
		domain,
		semanticid,
		subscription,
		config,
	}
}

// Count returns the count number of messages
func (s *service) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.Count")
	defer span.End()

	return s.repository.Count(ctx)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *service) GetChunksFromRemote(ctx context.Context, host string, timelines []string, pivot time.Time) (map[string]core.Chunk, error) {
	return s.repository.GetChunksFromRemote(ctx, host, timelines, pivot)
}

func (s *service) NormalizeTimelineID(ctx context.Context, timeline string) (string, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.NormalizeTimelineID")
	defer span.End()

	split := strings.Split(timeline, "@")
	id := split[0]
	domain := s.config.Concurrent.FQDN
	if len(split) == 2 {
		if core.IsCCID(split[1]) {
			entity, err := s.entity.Get(ctx, split[1])
			if err != nil {
				span.SetAttributes(attribute.String("timeline", timeline))
				span.RecordError(err)
				return "", err
			}
			domain = entity.Domain
		} else {
			domain = split[1]
		}
	}

	if !cdid.IsSeemsCDID(id, 't') && domain == s.config.Concurrent.FQDN && core.IsCCID(split[1]) {
		target, err := s.semanticid.Lookup(ctx, id, split[1])
		if err != nil {
			span.SetAttributes(attribute.String("timeline", timeline))
			span.RecordError(errors.Wrap(err, "failed to lookup semanticID"))
			return "", err
		}
		id = target
	}

	return fmt.Sprintf("%s@%s", id, domain), nil
}

// GetChunks returns chunks by timelineID and time
func (s *service) GetChunks(ctx context.Context, timelines []string, until time.Time) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.GetChunks")
	defer span.End()

	// normalize timelineID and validate
	for i, timeline := range timelines {
		normalized, err := s.NormalizeTimelineID(ctx, timeline)
		if err != nil {
			continue
		}
		timelines[i] = normalized
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

func (s *service) GetRecentItemsFromSubscription(ctx context.Context, subscription string, until time.Time, limit int) ([]core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.GetRecentItemsFromSubscription")
	defer span.End()

	sub, err := s.subscription.GetSubscription(ctx, subscription)
	if err != nil {
		return nil, err
	}

	timelines := make([]string, 0)
	for _, t := range sub.Items {
		timelines = append(timelines, t.ID)
	}

	return s.GetRecentItems(ctx, timelines, until, limit)
}

// GetRecentItems returns recent message from timelines
func (s *service) GetRecentItems(ctx context.Context, timelines []string, until time.Time, limit int) ([]core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.GetRecentItems")
	defer span.End()

	// normalize timelineID and validate
	for i, timeline := range timelines {
		normalized, err := s.NormalizeTimelineID(ctx, timeline)
		if err != nil {
			continue
		}
		timelines[i] = normalized
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
		if !m[elem.ResourceID] {
			m[elem.ResourceID] = true
			uniq = append(uniq, elem)
		}
	}

	sort.Slice(uniq, func(l, r int) bool {
		return uniq[l].CDate.After(uniq[r].CDate)
	})

	chopped := uniq[:min(len(uniq), limit)]

	return chopped, nil
}

func (s *service) GetImmediateItemsFromSubscription(ctx context.Context, subscription string, since time.Time, limit int) ([]core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.GetImmediateItemsFromSubscription")
	defer span.End()

	sub, err := s.subscription.GetSubscription(ctx, subscription)
	if err != nil {
		return nil, err
	}

	timelines := make([]string, 0)
	for _, t := range sub.Items {
		timelines = append(timelines, t.ID)
	}

	return s.GetImmediateItems(ctx, timelines, since, limit)
}

// GetImmediateItems returns immediate message from timelines
func (s *service) GetImmediateItems(ctx context.Context, timelines []string, since time.Time, limit int) ([]core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.GetImmediateItems")
	defer span.End()

	return nil, fmt.Errorf("not implemented")
}

// Post posts events to the local timeline.
func (s *service) PostItem(ctx context.Context, timeline string, item core.TimelineItem, document, signature string) (core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.PostItem")
	defer span.End()

	span.SetAttributes(attribute.String("timeline", timeline))

	query := strings.Split(timeline, "@")
	if len(query) != 2 {
		return core.TimelineItem{}, fmt.Errorf("Invalid format: %v", timeline)
	}

	timelineID, timelineHost := query[0], query[1]

	if core.IsCCID(timelineHost) {
		requester, err := s.entity.Get(ctx, timelineHost)
		if err != nil {
			span.RecordError(err)
			return core.TimelineItem{}, err
		}
		timelineHost = requester.Domain
	}

	if !cdid.IsSeemsCDID(timelineID, 't') && timelineHost == s.config.Concurrent.FQDN && core.IsCCID(query[1]) {
		target, err := s.semanticid.Lookup(ctx, timelineID, query[1])
		if err != nil {
			span.RecordError(err)
			return core.TimelineItem{}, err
		}
		timelineID = target
	}

	item.TimelineID = timelineID

	author := item.Owner
	if item.Author != nil {
		author = *item.Author
	}

	if timelineHost != s.config.Concurrent.FQDN {
		span.RecordError(fmt.Errorf("Remote timeline is not supported"))
		return core.TimelineItem{}, fmt.Errorf("Program error: remote timeline is not supported")
	}

	// check if the user has write access to the timeline
	if !s.HasWriteAccess(ctx, timelineID, author) {
		slog.InfoContext(
			ctx, "failed to post to timeline",
			slog.String("type", "audit"),
			slog.String("principal", author),
			slog.String("timeline", timelineID),
			slog.String("module", "timeline"),
		)
		return core.TimelineItem{}, fmt.Errorf("You don't have write access to %v", timelineID)
	}

	slog.DebugContext(
		ctx, fmt.Sprintf("post to local timeline: %v to %v", item.ResourceID, timelineID),
		slog.String("module", "timeline"),
	)

	// add to timeline
	created, err := s.repository.CreateItem(ctx, item)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create item", slog.String("error", err.Error()), slog.String("module", "timeline"))
		span.RecordError(err)
		return core.TimelineItem{}, err
	}

	return created, nil
}

func (s *service) PublishEvent(ctx context.Context, event core.Event) error {
	ctx, span := tracer.Start(ctx, "Timeline.Service.PublishEvent")
	defer span.End()

	return s.repository.PublishEvent(ctx, event)
}

func (s *service) Event(ctx context.Context, mode core.CommitMode, document, signature string) (core.Event, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.Event")
	defer span.End()

	var doc core.EventDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Event{}, err
	}

	event := core.Event{
		Timeline:  doc.Timeline,
		Item:      doc.Item,
		Document:  doc.Document,
		Signature: doc.Signature,
		Resource:  doc.Resource,
	}

	return event, s.repository.PublishEvent(ctx, event)
}

// Create updates timeline information
func (s *service) UpsertTimeline(ctx context.Context, mode core.CommitMode, document, signature string) (core.Timeline, error) {
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
		_, err = s.semanticid.Name(ctx, doc.SemanticID, doc.Signer, saved.ID, document, signature)
		if err != nil {
			return core.Timeline{}, err
		}
	}

	saved.ID = saved.ID + "@" + s.config.Concurrent.FQDN

	return saved, err
}

// Get returns timeline information by ID
func (s *service) GetTimeline(ctx context.Context, key string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.GetTimeline")
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
	ctx, span := tracer.Start(ctx, "Timeline.Service.ListTimelineBySchema")
	defer span.End()

	timelines, err := s.repository.ListTimelineBySchema(ctx, schema)
	for i := 0; i < len(timelines); i++ {
		timelines[i].ID = timelines[i].ID + "@" + s.config.Concurrent.FQDN
	}
	return timelines, err
}

// TimelineListByAuthor returns timelineList by author
func (s *service) ListTimelineByAuthor(ctx context.Context, author string) ([]core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.ListTimelineByAuthor")
	defer span.End()

	timelines, err := s.repository.ListTimelineByAuthor(ctx, author)
	for i := 0; i < len(timelines); i++ {
		timelines[i].ID = timelines[i].ID + "@" + s.config.Concurrent.FQDN
	}
	return timelines, err
}

// GetItem returns timeline element by ID
func (s *service) GetItem(ctx context.Context, timeline string, id string) (core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.GetItem")
	defer span.End()

	return s.repository.GetItem(ctx, timeline, id)
}

// Remove removes timeline element by ID
func (s *service) RemoveItem(ctx context.Context, timeline string, id string) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.RemoveItem")
	defer span.End()

	s.repository.DeleteItem(ctx, timeline, id)
}

// Delete deletes
func (s *service) DeleteTimeline(ctx context.Context, mode core.CommitMode, document string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.DeleteTimeline")
	defer span.End()

	var doc core.DeleteDocument
	err := json.Unmarshal([]byte(document), &doc)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}

	deleteTarget, err := s.repository.GetTimeline(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}

	if deleteTarget.Author != doc.Signer {
		return core.Timeline{}, fmt.Errorf("You are not authorized to perform this action")
	}

	err = s.repository.DeleteTimeline(ctx, doc.Target)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}

	return deleteTarget, err
}

func (s *service) ListTimelineSubscriptions(ctx context.Context) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.ListTimelineSubscriptions")
	defer span.End()

	return s.repository.ListTimelineSubscriptions(ctx)
}

func (s *service) getTimelineAutoDomain(ctx context.Context, timelineID string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Service.getTimelineAutoDomain")
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
	ctx, span := tracer.Start(ctx, "Timeline.Service.HasWriteAccess")
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
	ctx, span := tracer.Start(ctx, "Timeline.Service.HasReadAccess")
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
