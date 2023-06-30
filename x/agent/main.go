// Package agent runs some scheduled tasks
package agent

import (
    "log"
    "time"
    "sync"
    "context"
    "net/url"
    "strings"
    "encoding/json"
	"github.com/redis/go-redis/v9"
    "github.com/gorilla/websocket"
	"github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/host"
    "github.com/totegamma/concurrent/x/entity"
)

// Agent is...
type Agent struct {
    rdb* redis.Client
    config util.Config
    host* host.Service
    entity* entity.Service
    mutex *sync.Mutex
    connections map[string]*websocket.Conn
}

// NewAgent is...
func NewAgent(rdb *redis.Client, config util.Config, host *host.Service, entity *entity.Service) *Agent {
    return &Agent{
        rdb,
        config,
        host,
        entity,
        &sync.Mutex{},
        make(map[string]*websocket.Conn),
    }
}


func (a *Agent) collectUsers(ctx context.Context) {
    hosts, _ := a.host.List(ctx)
    for _, host := range hosts {
        log.Printf("collect users for %v\n", host)
        a.entity.PullRemoteEntities(ctx, host)
    }
}


// Boot starts agent
func (a *Agent) Boot() {
    log.Printf("agent start!")
    ticker10 := time.NewTicker(10 * time.Second)
    ticker60 := time.NewTicker(60 * time.Second)
    go func() {
        for {
            select {
                case <-ticker10.C:
                    a.updateConnections(context.Background())
                    break
                case <- ticker60.C:
                    ctx, cancel := context.WithTimeout(context.Background(), 120 * time.Second)
                    defer cancel()
                    a.collectUsers(ctx)
                    break
            }
        }
    }()

}

func (a *Agent)updateConnections(ctx context.Context) {
    a.mutex.Lock()
    defer a.mutex.Unlock()

    query := a.rdb.PubSubChannels(ctx, "*")
    channels := query.Val()

    summarized := summarize(channels)
    var serverList []string
    for key := range summarized {
        if key == a.config.Concurrent.FQDN{
            continue
        }
        serverList = append(serverList, key)
    }

    // check all servers in the list
    for _, server := range serverList {
        if _, ok := a.connections[server]; !ok {
            // new server, create new connection
            u := url.URL{Scheme: "wss", Host: server, Path: "/api/v1/socket"}
            c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
            if err != nil {
                log.Printf("fail to dial: %v", err)
                continue
            }

            a.connections[server] = c

            // launch a new goroutine for handling incoming messages
            go func(c *websocket.Conn) {
                defer c.Close()
                for {
                    _, message, err := c.ReadMessage()
                    if err != nil {
                        log.Printf("fail to read message: %v", err)
                        return
                    }

                    var event streamEvent
                    err = json.Unmarshal(message, &event)
                    if err != nil {
                        log.Printf("fail to Unmarshall redis message: %v", err)
                    }

                    // publish message to Redis
                    err = a.rdb.Publish(ctx, event.Stream, string(message)).Err()
                    if err != nil {
                        log.Printf("fail to publish message to Redis: %v", err)
                    }
                }
            }(c)
        }
        request := channelRequest {
            summarized[server],
        }
        err := websocket.WriteJSON(a.connections[server], request)
        if err != nil {
            log.Printf("fail to send subscribe request to remote server %v: %v", server, err)
            delete(a.connections, server)
        }
    }

    // remove connections to servers that are no longer in the list
    for server, conn := range a.connections {
        if !isInList(server, serverList) {
            err := conn.Close()
            if err != nil {
                log.Printf("fail to close connection: %v", err)
            }
            delete(a.connections, server)
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
        fqdn := split[1]

        summary[fqdn] = append(summary[fqdn], item)
    }

    return summary
}

type channelRequest struct {
	Channels []string `json:"channels"`
}

type streamEvent struct {
    Stream string `json:"stream"`
    Type string `json:"type"`
    Action string `json:"action"`
}



