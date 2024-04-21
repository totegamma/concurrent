package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/schema"
	"github.com/totegamma/concurrent/x/socket"
	"github.com/totegamma/concurrent/x/util"
	"gorm.io/gorm"
	"slices"
)

// Repository is timeline repository interface
type Repository interface {
	GetTimeline(ctx context.Context, key string) (core.Timeline, error)
	GetTimelineFromRemote(ctx context.Context, host string, key string) (core.Timeline, error)
	UpsertTimeline(ctx context.Context, timeline core.Timeline) (core.Timeline, error)
	DeleteTimeline(ctx context.Context, key string) error

	GetItem(ctx context.Context, timelineID string, objectID string) (core.TimelineItem, error)
	CreateItem(ctx context.Context, item core.TimelineItem) (core.TimelineItem, error)
	DeleteItem(ctx context.Context, timelineID string, objectID string) error

	ListTimelineBySchema(ctx context.Context, schema string) ([]core.Timeline, error)
	ListTimelineByAuthor(ctx context.Context, author string) ([]core.Timeline, error)

	GetRecentItems(ctx context.Context, timelineID string, until time.Time, limit int) ([]core.TimelineItem, error)
	GetImmediateItems(ctx context.Context, timelineID string, since time.Time, limit int) ([]core.TimelineItem, error)

	GetChunksFromCache(ctx context.Context, timelines []string, chunk string) (map[string]core.Chunk, error)
	GetChunksFromDB(ctx context.Context, timelines []string, chunk string) (map[string]core.Chunk, error)
	GetChunkIterators(ctx context.Context, timelines []string, chunk string) (map[string]string, error)
	GetChunksFromRemote(ctx context.Context, host string, timelines []string, queryTime time.Time) (map[string]core.Chunk, error)
	SaveToCache(ctx context.Context, chunks map[string]core.Chunk, queryTime time.Time) error
	PublishEvent(ctx context.Context, event core.Event) error

	ListTimelineSubscriptions(ctx context.Context) (map[string]int64, error)
	Count(ctx context.Context) (int64, error)
}

type repository struct {
	db      *gorm.DB
	rdb     *redis.Client
	mc      *memcache.Client
	client  client.Client
	schema  schema.Service
	manager socket.Manager
	config  util.Config
}

// NewRepository creates a new timeline repository
func NewRepository(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client client.Client, schema schema.Service, manager socket.Manager, config util.Config) Repository {

	var count int64
	err := db.Model(&core.Timeline{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count timelines",
			slog.String("error", err.Error()),
		)
	}

	mc.Set(&memcache.Item{Key: "timeline_count", Value: []byte(strconv.FormatInt(count, 10))})

	return &repository{db, rdb, mc, client, schema, manager, config}
}

// Total returns the total number of messages
func (r *repository) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.Count")
	defer span.End()

	item, err := r.mc.Get("timeline_count")
	if err != nil {
		span.RecordError(err)
		return 0, err
	}

	count, err := strconv.ParseInt(string(item.Value), 10, 64)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	return count, nil
}

func (r *repository) PublishEvent(ctx context.Context, event core.Event) error {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.PublishEvent")
	defer span.End()

	jsonstr, _ := json.Marshal(event)

	err := r.rdb.Publish(context.Background(), event.Timeline, jsonstr).Err()
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(
			ctx, "fail to publish message to Redis",
			slog.String("error", err.Error()),
			slog.String("module", "timeline"),
		)
	}

	return nil
}

// GetTimelineFromRemote gets a timeline from remote
func (r *repository) GetTimelineFromRemote(ctx context.Context, host string, key string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetTimelineFromRemote")
	defer span.End()

	// check cache
	cacheKey := "timeline:" + key + "@" + host
	item, err := r.mc.Get(cacheKey)
	if err == nil {
		var timeline core.Timeline
		err = json.Unmarshal(item.Value, &timeline)
		if err == nil {
			return timeline, nil
		}
		span.RecordError(err)
	}

	timeline, err := r.client.GetTimeline(ctx, host, key)
	if err != nil {
		span.RecordError(err)
		return core.Timeline{}, err
	}

	// save to cache
	body, err := json.Marshal(timeline)
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(
			ctx, "fail to marshal timeline",
			slog.String("error", err.Error()),
			slog.String("module", "timeline"),
		)
		return core.Timeline{}, err
	}

	err = r.mc.Set(&memcache.Item{Key: cacheKey, Value: body, Expiration: 300}) // 5 minutes
	if err != nil {
		span.RecordError(err)
		slog.ErrorContext(
			ctx, "fail to save cache",
			slog.String("error", err.Error()),
			slog.String("module", "timeline"),
		)
	}

	return timeline, nil
}

func (r *repository) GetChunksFromRemote(ctx context.Context, host string, timelines []string, queryTime time.Time) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetChunksFromRemote")
	defer span.End()

	chunks, err := r.client.GetChunks(ctx, host, timelines, queryTime)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	currentSubsciptions := r.manager.GetAllRemoteSubs()

	cacheChunks := make(map[string]core.Chunk)
	for timelineID, chunk := range chunks {
		if slices.Contains(currentSubsciptions, timelineID) {
			cacheChunks[timelineID] = chunk
		}
	}

	err = r.SaveToCache(ctx, cacheChunks, queryTime)
	if err != nil {
		slog.ErrorContext(
			ctx, "fail to save cache",
			slog.String("error", err.Error()),
			slog.String("module", "timeline"),
		)
		span.RecordError(err)
		return nil, err
	}

	return chunks, nil
}

// SaveToCache saves items to cache
func (r *repository) SaveToCache(ctx context.Context, chunks map[string]core.Chunk, queryTime time.Time) error {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.SaveToCache")
	defer span.End()

	for timelineID, chunk := range chunks {
		//save iterator
		itrKey := "timeline:itr:all:" + timelineID + ":" + core.Time2Chunk(queryTime)
		r.mc.Set(&memcache.Item{Key: itrKey, Value: []byte(chunk.Key)})

		// save body
		slices.Reverse(chunk.Items)
		b, err := json.Marshal(chunk.Items)
		if err != nil {
			span.RecordError(err)
			return err
		}
		value := string(b[1:len(b)-1]) + ","
		err = r.mc.Set(&memcache.Item{Key: chunk.Key, Value: []byte(value)})
		if err != nil {
			span.RecordError(err)
			continue
		}
	}
	return nil
}

// GetChunksFromCache gets chunks from cache
func (r *repository) GetChunksFromCache(ctx context.Context, timelines []string, chunk string) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetChunksFromCache")
	defer span.End()

	targetKeyMap, err := r.GetChunkIterators(ctx, timelines, chunk)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	targetKeys := make([]string, 0)
	for _, targetKey := range targetKeyMap {
		targetKeys = append(targetKeys, targetKey)
	}

	if len(targetKeys) == 0 {
		return map[string]core.Chunk{}, nil
	}

	caches, err := r.mc.GetMulti(targetKeys)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make(map[string]core.Chunk)
	for _, timeline := range timelines {
		targetKey := targetKeyMap[timeline]
		cache, ok := caches[targetKey]
		if !ok || len(cache.Value) == 0 {
			continue
		}

		var items []core.TimelineItem
		cacheStr := string(cache.Value)
		cacheStr = cacheStr[:len(cacheStr)-1]
		cacheStr = "[" + cacheStr + "]"
		err = json.Unmarshal([]byte(cacheStr), &items)
		if err != nil {
			span.RecordError(err)
			continue
		}
		slices.Reverse(items)
		result[timeline] = core.Chunk{
			Key:   targetKey,
			Items: items,
		}
	}

	return result, nil
}

// GetChunksFromDB gets chunks from db and cache them
func (r *repository) GetChunksFromDB(ctx context.Context, timelines []string, chunk string) (map[string]core.Chunk, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetChunksFromDB")
	defer span.End()

	targetKeyMap, err := r.GetChunkIterators(ctx, timelines, chunk)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	targetKeys := make([]string, 0)
	for _, targetKey := range targetKeyMap {
		targetKeys = append(targetKeys, targetKey)
	}

	result := make(map[string]core.Chunk)
	for _, timeline := range timelines {
		targetKey := targetKeyMap[timeline]
		var items []core.TimelineItem
		chunkDate := core.Chunk2RecentTime(chunk)

		timelineID := timeline
		if strings.Contains(timelineID, "@") {
			timelineID = strings.Split(timelineID, "@")[0]
		}
		if len(timelineID) == 27 {
			if timelineID[0] != 't' {
				return nil, fmt.Errorf("timeline typed-id must start with 't'")
			}
			timelineID = timelineID[1:]
		}

		err = r.db.WithContext(ctx).Where("timeline_id = ? and c_date <= ?", timelineID, chunkDate).Order("c_date desc").Limit(100).Find(&items).Error
		if err != nil {
			span.RecordError(err)
			continue
		}

		// append domain to timelineID
		for i, item := range items {
			items[i].TimelineID = item.TimelineID + "@" + r.config.Concurrent.FQDN
		}

		result[timeline] = core.Chunk{
			Key:   targetKey,
			Items: items,
		}

		// キャッシュには逆順で保存する
		reversedItems := make([]core.TimelineItem, len(items))
		for i, item := range items {
			reversedItems[len(items)-i-1] = item
		}
		b, err := json.Marshal(reversedItems)
		if err != nil {
			span.RecordError(err)
			continue
		}
		cacheStr := string(b[1:len(b)-1]) + ","
		err = r.mc.Set(&memcache.Item{Key: targetKey, Value: []byte(cacheStr)})
		if err != nil {
			span.RecordError(err)
			continue
		}
	}

	return result, nil
}

// GetChunkIterators returns a list of iterated chunk keys
func (r *repository) GetChunkIterators(ctx context.Context, timelines []string, chunk string) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetChunkIterators")
	defer span.End()

	keys := make([]string, len(timelines))
	for i, timeline := range timelines {
		keys[i] = "timeline:itr:all:" + timeline + ":" + chunk
	}

	cache, err := r.mc.GetMulti(keys)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make(map[string]string)
	for i, timeline := range timelines {
		if cache[keys[i]] != nil { // hit
			result[timeline] = string(cache[keys[i]].Value)
		} else { // miss
			var item core.TimelineItem
			chunkTime := core.Chunk2RecentTime(chunk)
			dbid := timeline
			if strings.Contains(dbid, "@") {
				dbid = strings.Split(timeline, "@")[0]
			}
			if len(dbid) == 27 {
				if dbid[0] != 't' {
					return nil, fmt.Errorf("timeline typed-id must start with 't'")
				}
				dbid = dbid[1:]
			}
			err := r.db.WithContext(ctx).Where("timeline_id = ? and c_date <= ?", dbid, chunkTime).Order("c_date desc").First(&item).Error
			if err != nil {
				continue
			}
			key := "timeline:body:all:" + timeline + ":" + core.Time2Chunk(item.CDate)
			r.mc.Set(&memcache.Item{Key: keys[i], Value: []byte(key)})
			result[timeline] = key
		}
	}

	return result, nil
}

// GetItem returns a timeline item by TimelineID and ObjectID
func (r *repository) GetItem(ctx context.Context, timelineID string, objectID string) (core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetItem")
	defer span.End()

	var item core.TimelineItem
	err := r.db.WithContext(ctx).First(&item, "timeline_id = ? and resource_id = ?", timelineID, objectID).Error
	return item, err
}

// CreateItem creates a new timeline item
func (r *repository) CreateItem(ctx context.Context, item core.TimelineItem) (core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.CreateItem")
	defer span.End()

	if len(item.TimelineID) == 27 {
		if item.TimelineID[0] != 't' {
			return core.TimelineItem{}, fmt.Errorf("timeline typed-id must start with 't'")
		}
		item.TimelineID = item.TimelineID[1:]
	}

	err := r.db.WithContext(ctx).Create(&item).Error
	if err != nil {
		span.RecordError(err)
		return item, err
	}

	timelineID := "t" + item.TimelineID + "@" + r.config.Concurrent.FQDN

	json, err := json.Marshal(item)
	if err != nil {
		span.RecordError(err)
		return item, err
	}

	json = append(json, ',')

	itemChunk := core.Time2Chunk(item.CDate)
	cacheKey := "timeline:body:all:" + timelineID + ":" + itemChunk

	err = r.mc.Append(&memcache.Item{Key: cacheKey, Value: json})
	if err != nil {
		// キャッシュに保存できなかった場合、新しいチャンクをDBから作成する必要がある
		_, err = r.GetChunksFromDB(ctx, []string{timelineID}, itemChunk)

		// 再実行 (誤り: これをするとデータが重複するでしょ)
		/*
			err = r.mc.Append(&memcache.Item{Key: cacheKey, Value: json})
			if err != nil {
				// これは致命的にプログラムがおかしい
				log.Printf("failed to append cache: %v", err)
				span.RecordError(err)
				return item, err
			}
		*/

		if itemChunk != core.Time2Chunk(time.Now()) {
			// イテレータを更新する
			key := "timeline:itr:all:" + timelineID + ":" + itemChunk
			dest := "timeline:body:all:" + timelineID + ":" + itemChunk
			r.mc.Set(&memcache.Item{Key: key, Value: []byte(dest)})
		}
	}

	item.TimelineID = "t" + item.TimelineID

	return item, err
}

// DeleteItem deletes a timeline item
func (r *repository) DeleteItem(ctx context.Context, timelineID string, objectID string) error {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.DeleteItem")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.TimelineItem{}, "timeline_id = ? and resource_id = ?", timelineID, objectID).Error
}

// GetTimelineRecent returns a list of timeline items by TimelineID and time range
func (r *repository) GetRecentItems(ctx context.Context, timelineID string, until time.Time, limit int) ([]core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetRecentItems")
	defer span.End()

	var items []core.TimelineItem
	err := r.db.WithContext(ctx).Where("timeline_id = ? and c_date < ?", timelineID, until).Order("c_date desc").Limit(limit).Find(&items).Error
	return items, err
}

// GetTimelineImmediate returns a list of timeline items by TimelineID and time range
func (r *repository) GetImmediateItems(ctx context.Context, timelineID string, since time.Time, limit int) ([]core.TimelineItem, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetImmediateItems")
	defer span.End()

	var items []core.TimelineItem
	err := r.db.WithContext(ctx).Where("timeline_id = ? and c_date > ?", timelineID, since).Order("c_date asec").Limit(limit).Find(&items).Error
	return items, err
}

// GetTimeline returns a timeline by ID
func (r *repository) GetTimeline(ctx context.Context, key string) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.GetTimeline")
	defer span.End()

	if len(key) == 27 {
		if key[0] != 't' {
			return core.Timeline{}, fmt.Errorf("timeline typed-id must start with 't'")
		}
		key = key[1:]
	}

	var timeline core.Timeline
	err := r.db.WithContext(ctx).First(&timeline, "id = ?", key).Error

	schemaUrl, err := r.schema.IDToUrl(ctx, timeline.SchemaID)
	if err != nil {
		return timeline, err
	}
	timeline.Schema = schemaUrl

	timeline.ID = "t" + timeline.ID

	return timeline, err
}

// Create updates a timeline
func (r *repository) UpsertTimeline(ctx context.Context, timeline core.Timeline) (core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.UpsertTimeline")
	defer span.End()

	if len(timeline.ID) == 27 {
		if timeline.ID[0] != 't' {
			return core.Timeline{}, fmt.Errorf("timeline typed-id must start with 't'")
		}
		timeline.ID = timeline.ID[1:]
	}

	schemaID, err := r.schema.UrlToID(ctx, timeline.Schema)
	if err != nil {
		return timeline, err
	}
	timeline.SchemaID = schemaID

	err = r.db.WithContext(ctx).Save(&timeline).Error

	r.mc.Increment("timeline_count", 1)

	timeline.ID = "t" + timeline.ID

	return timeline, err
}

// GetListBySchema returns list of schemas by schema
func (r *repository) ListTimelineBySchema(ctx context.Context, schema string) ([]core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.ListTimelineBySchema")
	defer span.End()

	id, err := r.schema.UrlToID(ctx, schema)
	if err != nil {
		return []core.Timeline{}, err
	}

	var timelines []core.Timeline
	err = r.db.WithContext(ctx).Where("schema_id = ? and indexable = true", id).Find(&timelines).Error

	for i, timeline := range timelines {
		timelines[i].ID = "t" + timeline.ID
		schemaUrl, err := r.schema.IDToUrl(ctx, timeline.SchemaID)
		if err != nil {
			continue
		}
		timelines[i].Schema = schemaUrl
	}

	return timelines, err
}

// GetListByAuthor returns list of schemas by owner
func (r *repository) ListTimelineByAuthor(ctx context.Context, author string) ([]core.Timeline, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.ListTimelineByAuthor")
	defer span.End()

	var timelines []core.Timeline
	err := r.db.WithContext(ctx).Where("Author = ?", author).Find(&timelines).Error

	for i, timeline := range timelines {
		timelines[i].ID = "t" + timeline.ID
		schemaUrl, err := r.schema.IDToUrl(ctx, timeline.SchemaID)
		if err != nil {
			continue
		}
		timelines[i].Schema = schemaUrl
	}

	return timelines, err
}

// Delete deletes a timeline
func (r *repository) DeleteTimeline(ctx context.Context, timelineID string) error {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.DeleteTimeline")
	defer span.End()

	if len(timelineID) == 27 {
		if timelineID[0] != 't' {
			return fmt.Errorf("timeline typed-id must start with 't'")
		}
		timelineID = timelineID[1:]
	}

	r.mc.Decrement("timeline_count", 1)

	return r.db.WithContext(ctx).Delete(&core.Timeline{}, "id = ?", timelineID).Error
}

// List Timeline Subscriptions
func (r *repository) ListTimelineSubscriptions(ctx context.Context) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Timeline.Repository.ListTimelineSubscriptions")
	defer span.End()

	query_l := r.rdb.PubSubChannels(ctx, "*")
	timelines, err := query_l.Result()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	query_n := r.rdb.PubSubNumSub(ctx, timelines...)
	result, err := query_n.Result()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return result, nil
}
