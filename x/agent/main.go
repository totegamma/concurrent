// Package agent runs some scheduled tasks
package agent

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/host"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var tracer = otel.Tracer("agent")

// Agent is...
type Agent struct {
	rdb         *redis.Client
	config      util.Config
	host        *host.Service
	entity      *entity.Service
	mutex       *sync.Mutex
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
		a.pullRemoteEntities(ctx, host)
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
			case <-ticker60.C:
				ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
				defer cancel()
				a.collectUsers(ctx)
				break
			}
		}
	}()

}

func (a *Agent) updateConnections(ctx context.Context) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	query := a.rdb.PubSubChannels(ctx, "*")
	channels := query.Val()

	summarized := summarize(channels)
	var serverList []string
	for key := range summarized {
		if key == a.config.Concurrent.FQDN {
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
		request := channelRequest{
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

// PullRemoteEntities copies remote entities
func (a *Agent) pullRemoteEntities(ctx context.Context, remote core.Host) error {
	ctx, span := tracer.Start(ctx, "ServicePullRemoteEntities")
	defer span.End()

	requestTime := time.Now()
	req, err := http.NewRequest("GET", "https://"+remote.ID+"/api/v1/entity/list?since="+strconv.FormatInt(remote.LastScraped.Unix(), 10), nil) // TODO: add except parameter
	if err != nil {
		span.RecordError(err)
		return err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var remoteEntities []entity.SafeEntity
	json.Unmarshal(body, &remoteEntities)

	errored := false
	for _, entity := range remoteEntities {

		certs := entity.Certs
		if certs == "" {
			certs = "null"
		}

		hostname := entity.Host
		if hostname == "" {
			hostname = remote.ID
		}

		if hostname == a.config.Concurrent.FQDN {
			continue
		}

		err := a.entity.Upsert(ctx, &core.Entity{
			ID:    entity.ID,
			Host:  hostname,
			Certs: certs,
			Meta:  "null",
		})

		if err != nil {
			span.RecordError(err)
			errored = true
			log.Println(err)
		}
	}

	if !errored {
		a.host.UpdateScrapeTime(ctx, remote.ID, requestTime)
	}

	return nil
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
	Type   string `json:"type"`
	Action string `json:"action"`
}
