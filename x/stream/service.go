package stream

import (
    "context"
    "encoding/json"
    "log"
    "sort"
    "strconv"
    "strings"

    "github.com/redis/go-redis/v9"
    "github.com/totegamma/concurrent/x/entity"
    "github.com/totegamma/concurrent/x/util"
)

// Service is stream service
type Service struct {
    client* redis.Client
    repository* Repository
    entity* entity.Service
}

// NewService is for wire.go
func NewService(client *redis.Client, repository *Repository, entity *entity.Service) *Service {
    return &Service{ client, repository, entity }
}

var ctx = context.Background()

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetRecent returns recent message from streams
func (s *Service) GetRecent(streams []string, limit int) []Element {
    var messages []redis.XMessage
    for _, stream := range streams {
        cmd := s.client.XRevRangeN(ctx, stream, "+", "-", int64(limit))
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
    chopped := uniq[:min(len(uniq), limit)]
    result := []Element{}

    for _, elem := range chopped {
        result = append(result, Element{
            Timestamp: elem.ID,
            ID: elem.Values["id"].(string),
            Author: elem.Values["author"].(string),
            Host: s.entity.ResolveHost(elem.Values["author"].(string)),
        })
    }

    return result
}

// GetRange returns specified range messages from streams
func (s *Service) GetRange(streams []string, since string ,until string, limit int) []Element {
    var messages []redis.XMessage
    for _, stream := range streams {
        cmd := s.client.XRevRangeN(ctx, stream, until, since, int64(limit))
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
    chopped := uniq[:min(len(uniq), limit)]
    result := []Element{}

    for _, elem := range chopped {
        result = append(result, Element{
            Timestamp: elem.ID,
            ID: elem.Values["id"].(string),
            Author: elem.Values["author"].(string),
            Host: s.entity.ResolveHost(elem.Values["author"].(string)),
        })
    }

    return result
}

// Post posts to stream
func (s *Service) Post(stream string, id string, author string) string {
    cmd := s.client.XAdd(ctx, &redis.XAddArgs{
        Stream: stream,
        ID: "*",
        Values: map[string]interface{}{
            "id": id,
            "author": author,
        },
    })

    return cmd.Val()
}


// Upsert updates stream information
func (s *Service) Upsert(objectStr string, signature string, id string) (string, error) {
    var object signedObject
    err := json.Unmarshal([]byte(objectStr), &object)
    if err != nil {
        return "", err
    }

    if err := util.VerifySignature(objectStr, object.Signer, signature); err != nil {
        log.Println("verify signature err: ", err)
        return "", err
    }

    stream := Stream {
        ID: id,
        Author: object.Signer,
        Maintainer: object.Maintainer,
        Writer: object.Writer,
        Reader: object.Reader,
        Schema: object.Schema,
        Payload: objectStr,
        Signature: signature,
    }

    s.repository.Upsert(&stream)
    return stream.ID, nil
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

