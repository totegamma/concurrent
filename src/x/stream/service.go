package stream

import (
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
    cmd := s.client.XRead(redis_ctx, &redis.XReadArgs{
        Streams: streams,
        Count: 64,
    })
    var messages []redis.XMessage
    for _, elem := range cmd.Val() {
        messages = append(messages, elem.Messages...)
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

