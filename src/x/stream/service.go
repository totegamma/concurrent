package stream

import (
    "fmt"
    "time"
    "context"
    "errors"
    "crypto/rand"
    "github.com/redis/go-redis/v9"
)

type StreamService struct {
}

func NewStreamService() StreamService {
    return StreamService{}
}

var redis_ctx = context.Background()

func (s *StreamService) PostRedis() {
    rdb := redis.NewClient(&redis.Options{
        Addr:     "localhost:6379",
        Password: "", // no password set
        DB:       0,  // use default DB
    })

    message, err := MakeRandomStr(10)
    fmt.Println(message)

    content := redis.Z {
        Score: float64(time.Now().UnixMicro()),
        Member: message,
    }

    err = rdb.ZAdd(redis_ctx, "user/test", content).Err()
    if err != nil {
        panic(err)
    }

    cmd := rdb.XAdd(redis_ctx, &redis.XAddArgs{
        Stream: "user_stream",
        ID: "*",
        Values: map[string]interface{}{
            "timestamp": time.Now().UnixMicro(),
            "message": message,
        },
    })
    fmt.Printf("cmd: %v\n", cmd);

    vals, err := rdb.ZRevRangeByScore(redis_ctx, "user/test", &redis.ZRangeBy{
        Min: "-inf",
        Max: "+inf",
        Offset: 0,
        Count: 3,
    }).Result()
    fmt.Printf("%v\n", vals);
}

func MakeRandomStr(digit uint32) (string, error) {
    const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

    // 乱数を生成
    b := make([]byte, digit)
    if _, err := rand.Read(b); err != nil {
        return "", errors.New("unexpected error...")
    }

    // letters からランダムに取り出して文字列を生成
    var result string
    for _, v := range b {
        // index が letters の長さに収まるように調整
        result += string(letters[int(v)%len(letters)])
    }
    return result, nil
}

