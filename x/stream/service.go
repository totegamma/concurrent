package stream

import (
    "log"
    "sort"
    "strconv"
    "strings"
    "context"
    "github.com/redis/go-redis/v9"
)

// Service is stream service
type Service struct {
    client* redis.Client
    repository* Repository
}

// NewService is for wire.go
func NewService(client *redis.Client, repository *Repository) Service {
    return Service{ client, repository }
}

var ctx = context.Background()

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetRecent returns recent message from streams
func (s *Service) GetRecent(streams []string) []redis.XMessage {
    var messages []redis.XMessage
    for _, stream := range streams {
        cmd := s.client.XRevRangeN(ctx, stream, "+", "-", 16)
        messages = append(messages, cmd.Val()...)
    }
    m := make(map[string]bool)
    uniq := [] redis.XMessage{}
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
    return uniq[:min(len(uniq),16)]
}

// GetRange returns specified range messages from streams
func (s *Service) GetRange(streams []string, since string ,until string, limit int64) []redis.XMessage {
    var messages []redis.XMessage
    for _, stream := range streams {
        cmd := s.client.XRevRangeN(ctx, stream, until, since, limit)
        messages = append(messages, cmd.Val()...)
    }
    m := make(map[string]bool)
    uniq := [] redis.XMessage{}
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
    return uniq[:min(len(uniq), int(limit))]
}

// Post posts to stream
func (s *Service) Post(stream string, id string) string {
    cmd := s.client.XAdd(ctx, &redis.XAddArgs{
        Stream: stream,
        ID: "*",
        Values: map[string]interface{}{
            "id": id,
        },
    })

    return cmd.Val()
}


// Upsert updates stream information
func (s *Service) Upsert(stream *Stream) {
    s.repository.Upsert(stream)
}

// Get returns stream information by ID
func (s *Service) Get(key string) Stream {
    return s.repository.Get(key)
}

// StreamListBySchema returns streamList by schema
func (s *Service) StreamListBySchema(schema string) []Stream {
    streams := s.repository.GetList(schema)
    return streams
}

// Delete deletes 
func (s *Service) Delete(stream string, id string) {
    cmd := s.client.XDel(ctx, stream, id)
    log.Println(cmd)
}

