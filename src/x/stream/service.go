package stream

import (
    "log"
    "sort"
    "strconv"
    "strings"
    "context"
    "github.com/redis/go-redis/v9"
)

type StreamService struct {
    client* redis.Client
    repository* Repository
}

func NewStreamService(client *redis.Client, repository *Repository) StreamService {
    return StreamService{ client, repository }
}

var redis_ctx = context.Background()

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *StreamService) GetRecent(streams []string) []redis.XMessage {
    var messages []redis.XMessage
    for _, stream := range streams {
        cmd := s.client.XRevRangeN(redis_ctx, stream, "+", "-", 16)
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

func (s *StreamService) GetRange(streams []string, since string ,until string, limit int64) []redis.XMessage {
    var messages []redis.XMessage
    for _, stream := range streams {
        cmd := s.client.XRevRangeN(redis_ctx, stream, until, since, limit)
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

func (s *StreamService) Post(stream string, id string) string {
    cmd := s.client.XAdd(redis_ctx, &redis.XAddArgs{
        Stream: stream,
        ID: "*",
        Values: map[string]interface{}{
            "id": id,
        },
    })

    return cmd.Val()
}

func (s *StreamService) StreamList() []string {
    cmd := s.client.Keys(redis_ctx, "*")
    return cmd.Val()
}

func (s *StreamService) Upsert(stream *Stream) {
    s.repository.Upsert(stream)
}

func (s *StreamService) Get(key string) Stream {
    return s.repository.Get(key)
}

func (s *StreamService) StreamListBySchema(schema string) []Stream {
    streams := s.repository.GetList(schema)
    return streams
}

func (s *StreamService) Delete(stream string, id string) {
    cmd := s.client.XDel(redis_ctx, stream, id)
    log.Println(cmd)
}

