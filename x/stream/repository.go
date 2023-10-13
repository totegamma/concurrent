package stream

import (
	"log"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/x/core"
	"golang.org/x/exp/slices"
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

	GetMultiChunk(ctx context.Context, streams []string, chunk string) (map[string][]core.StreamItem, error)
	GetChunkIterators(ctx context.Context, streams []string, chunk string) (map[string]string, error)
}

type repository struct {
	db *gorm.DB
	mc *memcache.Client
}

// NewRepository creates a new stream repository
func NewRepository(db *gorm.DB, mc *memcache.Client) Repository {
	return &repository{db, mc}
}

func (r *repository) GetMultiChunk(ctx context.Context, streams []string, chunk string) (map[string][]core.StreamItem, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetMultiChunk")
	defer span.End()

	log.Printf("GetMultiChunk: %v, %v", streams, chunk)

	targetKeyMap, err := r.GetChunkIterators(ctx, streams, chunk)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	log.Printf("GetMultiChunk: %v", targetKeyMap)

	targetKeys := make([]string, 0)
	for _, targetKey := range targetKeyMap {
		targetKeys = append(targetKeys, targetKey)
	}

	log.Printf("GetMultiChunk: %v", targetKeys)

	caches, err := r.mc.GetMulti(targetKeys)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	log.Printf("GetMultiChunk: %v", caches)

	result := make(map[string][]core.StreamItem)

	for _, stream := range streams {
		targetKey := targetKeyMap[stream]
		if caches[targetKey] != nil { // hit
			log.Printf("GetMultiChunk: hit %v", targetKey)
			var items []core.StreamItem
			cache := string(caches[targetKey].Value)
			cache = cache[:len(cache)-1]
			cache = "[" + cache + "]"
			err = json.Unmarshal([]byte(cache), &items)
			if err != nil {
				span.RecordError(err)
				log.Printf("GetMultiChunk UnmarshalError: %v", cache)
				log.Printf("Original: %v", string(caches[targetKey].Value))
				return nil, err
			}
			result[stream] = items
		} else { // miss
			log.Printf("GetMultiChunk: miss %v", targetKey)
			var items []core.StreamItem
			chunkInt, err := strconv.Atoi(chunk)
			chunkDate := time.Unix(int64(chunkInt), 0)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			err = r.db.WithContext(ctx).Where("stream_id = ? and c_date <= ?", stream, chunkDate).Order("c_date desc").Limit(100).Find(&items).Error
			if err != nil {
				span.RecordError(err)
				items = make([]core.StreamItem, 0)
			}
			b, err := json.Marshal(items) // like "[{...},{...},{...}]"
			if err != nil {
				span.RecordError(err)
				return nil, err
			}
			// strip the first and last characters
			newcache := string(b[1 : len(b)-1]) + "," // like "{...},{...},{...},"
			r.mc.Set(&memcache.Item{Key: targetKey, Value: []byte(newcache)})
			result[stream] = items
		}
	}
	return result, nil
}

// GetChunkIterators returns a list of iterated chunk keys
func (r *repository) GetChunkIterators(ctx context.Context, streams []string, chunk string) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetChunkIterators")
	defer span.End()

	log.Printf("GetChunkIterators0: %v, %v", streams, chunk)

	chunkInt, err := strconv.Atoi(chunk)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	chunkDate := time.Unix(int64(chunkInt), 0)

	keys := make([]string, len(streams))
	for i, stream := range streams {
		keys[i] = "stream:itr:all:" + stream + ":" + chunk
	}

	log.Printf("GetChunkIterators1: %v", keys)

	cache, err := r.mc.GetMulti(keys)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	log.Printf("GetChunkIterators2: %v", cache)

	result := make(map[string]string)
	for i, stream := range streams {
		if cache[keys[i]] != nil { // hit
			result[stream] = string(cache[keys[i]].Value)
		} else { // miss
			var item core.StreamItem
			err := r.db.WithContext(ctx).Where("stream_id = ? and c_date <= ?", stream, chunkDate).Order("c_date desc").First(&item).Error
			if err != nil {
				continue
			}
			log.Printf("GetChunkIterator-dbread: %v", item)
			key := "stream:body:all:" + stream + ":" + ChunkDate(item.CDate)
			r.mc.Set(&memcache.Item{Key: keys[i], Value: []byte(key)})
			result[stream] = key
		}
	}

	log.Printf("GetChunkIterators3: %v", result)
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
