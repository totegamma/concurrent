package stream

import (
	"log"
	"context"
	"encoding/json"
	"time"
	"strings"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"slices"
	"gorm.io/gorm"
)

// Repository is stream repository interface
type Repository interface {
	GetStream(ctx context.Context, key string) (core.Stream, error)
	CreateStream(ctx context.Context, stream core.Stream) (core.Stream, error)
	UpdateStream(ctx context.Context, stream core.Stream) (core.Stream, error)
	DeleteStream(ctx context.Context, key string) error

	GetItem(ctx context.Context, streamID string, objectID string) (core.StreamItem, error)
	CreateItem(ctx context.Context, item core.StreamItem) (core.StreamItem, error)
	DeleteItem(ctx context.Context, streamID string, objectID string) error

	ListStreamBySchema(ctx context.Context, schema string) ([]core.Stream, error)
	ListStreamByAuthor(ctx context.Context, author string) ([]core.Stream, error)
	HasWriteAccess(ctx context.Context, key string, author string) bool
	HasReadAccess(ctx context.Context, key string, author string) bool

	GetRecentItems(ctx context.Context, streamID string, until time.Time, limit int) ([]core.StreamItem, error)
	GetImmediateItems(ctx context.Context, streamID string, since time.Time, limit int) ([]core.StreamItem, error)

	GetChunksFromCache(ctx context.Context, streams []string, chunk string) (map[string]Chunk, error)
	GetChunksFromDB(ctx context.Context, streams []string, chunk string) (map[string]Chunk, error)
	GetChunkIterators(ctx context.Context, streams []string, chunk string) (map[string]string, error)

	SaveToCache(ctx context.Context, chunks map[string]Chunk, queryTime time.Time) error
}

type repository struct {
	db *gorm.DB
	mc *memcache.Client
	config util.Config
}

// NewRepository creates a new stream repository
func NewRepository(db *gorm.DB, mc *memcache.Client, config util.Config) Repository {
	return &repository{db, mc, config}
}

// SaveToCache saves items to cache
func (r *repository) SaveToCache(ctx context.Context, chunks map[string]Chunk, queryTime time.Time) error {
	ctx, span := tracer.Start(ctx, "RepositorySaveToCache")
	defer span.End()

	for streamID, chunk := range chunks {
		//save iterator
		itrKey := "stream:itr:all:" + streamID + ":" + Time2Chunk(queryTime)
		r.mc.Set(&memcache.Item{Key: itrKey, Value: []byte(chunk.Key)})

		// save body
		slices.Reverse(chunk.Items)
		b, err := json.Marshal(chunk.Items)
		if err != nil {
			span.RecordError(err)
			return err
		}
		value := string(b[1 : len(b)-1]) + ","
		err = r.mc.Set(&memcache.Item{Key: chunk.Key, Value: []byte(value)})
		if err != nil {
			span.RecordError(err)
			continue
		}
	}
	return nil
}

// GetChunksFromCache gets chunks from cache
func (r *repository) GetChunksFromCache(ctx context.Context, streams []string, chunk string) (map[string]Chunk, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetChunksFromCache")
	defer span.End()

	targetKeyMap, err := r.GetChunkIterators(ctx, streams, chunk)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	log.Println("targetKeyMap", targetKeyMap)

	targetKeys := make([]string, 0)
	for _, targetKey := range targetKeyMap {
		targetKeys = append(targetKeys, targetKey)
	}

	if len(targetKeys) == 0 {
		return map[string]Chunk{}, nil
	}

	caches, err := r.mc.GetMulti(targetKeys)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make(map[string]Chunk)
	for _, stream := range streams {
		targetKey := targetKeyMap[stream]
		cache, ok := caches[targetKey]
		if !ok || len(cache.Value) == 0 {
			continue
		}

		var items []core.StreamItem
		cacheStr := string(cache.Value)
		cacheStr = cacheStr[:len(cacheStr)-1]
		cacheStr = "[" + cacheStr + "]"
		err = json.Unmarshal([]byte(cacheStr), &items)
		if err != nil {
			span.RecordError(err)
			continue
		}
		slices.Reverse(items)
		result[stream] = Chunk {
			Key: targetKey,
			Items: items,
		}
	}

	return result, nil
}

// GetChunksFromDB gets chunks from db and cache them
func (r *repository) GetChunksFromDB(ctx context.Context, streams []string, chunk string) (map[string]Chunk, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetChunksFromDB")
	defer span.End()

	targetKeyMap, err := r.GetChunkIterators(ctx, streams, chunk)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	targetKeys := make([]string, 0)
	for _, targetKey := range targetKeyMap {
		targetKeys = append(targetKeys, targetKey)
	}

	result := make(map[string]Chunk)
	for _, stream := range streams {
		targetKey := targetKeyMap[stream]
		var items []core.StreamItem
		chunkDate := Chunk2RecentTime(chunk)

		streamID := stream
		if strings.Contains(streamID, "@") {
			streamID = strings.Split(streamID, "@")[0]
		}

		err = r.db.WithContext(ctx).Where("stream_id = ? and c_date <= ?", streamID, chunkDate).Order("c_date desc").Limit(100).Find(&items).Error
		if err != nil {
			span.RecordError(err)
			continue
		}
		result[stream] = Chunk {
			Key: targetKey,
			Items: items,
		}

		// キャッシュには逆順で保存する
		reversedItems := make([]core.StreamItem, len(items))
		for i, item := range items {
			reversedItems[len(items)-i-1] = item
		}
		b, err := json.Marshal(reversedItems)
		if err != nil {
			span.RecordError(err)
			continue
		}
		cacheStr := string(b[1 : len(b)-1]) + ","
		err = r.mc.Set(&memcache.Item{Key: targetKey, Value: []byte(cacheStr)})
		if err != nil {
			span.RecordError(err)
			continue
		}
	}

	return result, nil
}

// GetChunkIterators returns a list of iterated chunk keys
func (r *repository) GetChunkIterators(ctx context.Context, streams []string, chunk string) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetChunkIterators")
	defer span.End()

	keys := make([]string, len(streams))
	for i, stream := range streams {
		keys[i] = "stream:itr:all:" + stream + ":" + chunk
	}

	cache, err := r.mc.GetMulti(keys)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make(map[string]string)
	for i, stream := range streams {
		if cache[keys[i]] != nil { // hit
			result[stream] = string(cache[keys[i]].Value)
		} else { // miss
			var item core.StreamItem
			chunkTime := Chunk2RecentTime(chunk)
			dbid := stream
			if strings.Contains(dbid, "@") {
				dbid = strings.Split(stream, "@")[0]
			}
			err := r.db.WithContext(ctx).Where("stream_id = ? and c_date <= ?", dbid, chunkTime).Order("c_date desc").First(&item).Error
			if err != nil {
				continue
			}
			key := "stream:body:all:" + stream + ":" + Time2Chunk(item.CDate)
			r.mc.Set(&memcache.Item{Key: keys[i], Value: []byte(key)})
			result[stream] = key
		}
	}

	return result, nil
}

// GetItem returns a stream item by StreamID and ObjectID
func (r *repository) GetItem(ctx context.Context, streamID string, objectID string) (core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetItem")
	defer span.End()

	var item core.StreamItem
	err := r.db.WithContext(ctx).First(&item, "stream_id = ? and object_id = ?", streamID, objectID).Error
	return item, err
}

// CreateItem creates a new stream item
func (r *repository) CreateItem(ctx context.Context, item core.StreamItem) (core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreateItem")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&item).Error

	streamID := item.StreamID + "@" + r.config.Concurrent.FQDN

	json, err := json.Marshal(item)
	if err != nil {
		span.RecordError(err)
		return item, err
	}

	json = append(json, ',')

	cacheKey := "stream:body:all:" + streamID + ":" + Time2Chunk(item.CDate)

	err = r.mc.Append(&memcache.Item{Key: cacheKey, Value: json})
	if err != nil {
		return item, nil // XXX: キャッシュに保存できなくてもエラーにしない(本当に?)
	}

	// chunk Iteratorを更新
	// TOOD: 本当は今からInsertするitemのchunkが本当に最新かどうかを確認する必要がある
	key := "stream:itr:all:" + streamID + ":" + Time2Chunk(item.CDate)
	dest := "stream:body:all:" + streamID + ":" + Time2Chunk(item.CDate)
	r.mc.Set(&memcache.Item{Key: key, Value: []byte(dest)})

	return item, err
}

// DeleteItem deletes a stream item
func (r *repository) DeleteItem(ctx context.Context, streamID string, objectID string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDeleteItem")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.StreamItem{}, "stream_id = ? and object_id = ?", streamID, objectID).Error
}

// GetStreamRecent returns a list of stream items by StreamID and time range
func (r *repository) GetRecentItems(ctx context.Context, streamID string, until time.Time, limit int) ([]core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetStreamRecent")
	defer span.End()

	var items []core.StreamItem
	err := r.db.WithContext(ctx).Where("stream_id = ? and c_date < ?", streamID, until).Order("c_date desc").Limit(limit).Find(&items).Error
	return items, err
}

// GetStreamImmediate returns a list of stream items by StreamID and time range
func (r *repository) GetImmediateItems(ctx context.Context, streamID string, since time.Time, limit int) ([]core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetStreamImmediate")
	defer span.End()

	var items []core.StreamItem
	err := r.db.WithContext(ctx).Where("stream_id = ? and c_date > ?", streamID, since).Order("c_date asec").Limit(limit).Find(&items).Error
	return items, err
}

// GetStream returns a stream by ID
func (r *repository) GetStream(ctx context.Context, key string) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetStream")
	defer span.End()

	var stream core.Stream
	err := r.db.WithContext(ctx).First(&stream, "id = ?", key).Error
	return stream, err
}

// Create updates a stream
func (r *repository) CreateStream(ctx context.Context, stream core.Stream) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreateStream")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&stream).Error
	return stream, err
}

// Update updates a stream
func (r *repository) UpdateStream(ctx context.Context, stream core.Stream) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryUpdateStream")
	defer span.End()

	var obj core.Stream
	err := r.db.WithContext(ctx).First(&obj, "id = ?", stream.ID).Error
	if err != nil {
		return core.Stream{}, err
	}
	err = r.db.WithContext(ctx).Model(&obj).Updates(stream).Error
	return stream, err
}

// GetListBySchema returns list of schemas by schema
func (r *repository) ListStreamBySchema(ctx context.Context, schema string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryListStream")
	defer span.End()

	var streams []core.Stream
	err := r.db.WithContext(ctx).Where("Schema = ? and visible = true", schema).Find(&streams).Error
	return streams, err
}

// GetListByAuthor returns list of schemas by owner
func (r *repository) ListStreamByAuthor(ctx context.Context, author string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryListStream")
	defer span.End()

	var streams []core.Stream
	err := r.db.WithContext(ctx).Where("Author = ?", author).Find(&streams).Error
	return streams, err
}

// Delete deletes a stream
func (r *repository) DeleteStream(ctx context.Context, streamID string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDeleteStream")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.Stream{}, "id = ?", streamID).Error
}

// HasWriteAccess returns true if the user has write access
func (r *repository) HasWriteAccess(ctx context.Context, streamID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "RepositoryHasWriteAccess")
	defer span.End()

	var stream core.Stream
	r.db.WithContext(ctx).First(&stream, "id = ?", streamID)
	if len(stream.Writer) == 0 {
		return true
	}
	return slices.Contains(stream.Writer, userAddress)
}

// HasReadAccess returns true if the user has read access
func (r *repository) HasReadAccess(ctx context.Context, streamID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "RepositoryHasReadAccess")
	defer span.End()

	var stream core.Stream
	r.db.WithContext(ctx).First(&stream, "id = ?", streamID)
	if len(stream.Reader) == 0 {
		return true
	}
	return slices.Contains(stream.Reader, userAddress)
}
