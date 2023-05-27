package socket

import (
    "log"
    "sync"
    "context"
    "net/url"
    "strings"
    "encoding/json"
	"github.com/redis/go-redis/v9"
    "github.com/gorilla/websocket"
	"github.com/totegamma/concurrent/x/util"
)

// Service is socket service
type Service struct {
    rdb* redis.Client
    mutex *sync.Mutex
    connections map[string]*websocket.Conn
    config util.Config
}

// NewService is for wire.go
func NewService(rdb *redis.Client, config util.Config) *Service {
    return &Service{
        rdb,
        &sync.Mutex{},
        make(map[string]*websocket.Conn),
        config,
    }
}

func (s *Service)UpdateConnections() {
    s.mutex.Lock()
    defer s.mutex.Unlock()

    query := s.rdb.PubSubChannels(context.Background(), "*")
    channels := query.Val()

    summarized := summarize(channels)
    var serverList []string
    for key := range summarized {
        if key == s.config.FQDN{
            continue
        }
        serverList = append(serverList, key)
    }

    // check all servers in the list
    for _, server := range serverList {
        if _, ok := s.connections[server]; !ok {
            // new server, create new connection
            u := url.URL{Scheme: "wss", Host: server, Path: "/socket"}
            c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
            if err != nil {
                log.Printf("fail to dial: %v", err)
                continue
            }

            s.connections[server] = c

            // launch a new goroutine for handling incoming messages
            go func(c *websocket.Conn) {
                defer c.Close()
                for {
                    _, message, err := c.ReadMessage()
                    if err != nil {
                        log.Printf("fail to read message: %v", err)
                        return
                    }

                    var event StreamEvent
                    err = json.Unmarshal(message, &event)
                    if err != nil {
                        log.Printf("fail to Unmarshall redis message: %v", err)
                    }

                    // publish message to Redis
                    err = s.rdb.Publish(context.Background(), event.Stream, string(message)).Err()
                    if err != nil {
                        log.Printf("fail to publish message to Redis: %v", err)
                    }
                }
            }(c)
        }
        request := ChannelRequest {
            summarized[server],
        }
        err := websocket.WriteJSON(s.connections[server], request)
        if err != nil {
            log.Printf("fail to send subscribe request to remote server %v: %v", server, err)
        }
    }

    // remove connections to servers that are no longer in the list
    for server, conn := range s.connections {
        if !isInList(server, serverList) {
            err := conn.Close()
            if err != nil {
                log.Printf("fail to close connection: %v", err)
            }
            delete(s.connections, server)
        }
    }
}

func isInList(server string, list []string) bool {
    for _, s := range list {
        if s == server {
            return true
        }
    }
    return false
}

func summarize(input []string) map[string][]string {
    summary := make(map[string][]string)
    for _, item := range input {
        split := strings.Split(item, "@")
        if len(split) != 2 {
            log.Println("Invalid format: ", item)
            continue
        }
        id, fqdn := split[0], split[1]

        summary[fqdn] = append(summary[fqdn], id)
    }

    return summary
}


