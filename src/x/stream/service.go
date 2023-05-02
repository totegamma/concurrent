package stream

import (
    "fmt"
    "context"
    "github.com/redis/go-redis/v9"
)

type StreamService struct {
    client* redis.Client
}

func NewStreamService(client *redis.Client) StreamService {
    return StreamService{ client }
}

var redis_ctx = context.Background()

func (s *StreamService) GetRecent(streams []string) []redis.XMessage {
    var messages []redis.XMessage
    for _, stream := range streams {
        cmd := s.client.XRevRangeN(redis_ctx, stream, "+", "-", 64)
        messages = append(messages, cmd.Val()...)
    }
    return messages
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

func (s *StreamService) Delete(stream string, id string) {
    cmd := s.client.XDel(redis_ctx, stream, id)
    fmt.Println(cmd)
}

