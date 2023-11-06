package socket

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/totegamma/concurrent/x/util"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/stream"
)

// ソケット管理のルール
// 1. Subscribeのリクエストのうち外部のリクエストは増える方向にしか更新をかけない
// 2. 外から受信したイベントのうち、すでにキャッシュのキーがあるものに対してappendをかける
// 3. chunk更新タイミングで、外部リクエストの棚卸しを行う
// [誤り] 4. その際、継続している外部リクエストのキャッシュを新しく空で作っておく(なければ)

var ctx = context.Background()

var (
	pingInterval    = 10 * time.Second
	disconnectTimeout = 30 * time.Second
)

type Manager interface {
	Subscribe(conn *websocket.Conn, streams []string)
}

type manager struct {
	mc *memcache.Client
	rdb  *redis.Client
	stream stream.Service
	config  util.Config

	clientSubs map[*websocket.Conn][]string
	remoteSubs map[string][]string
	remoteConns map[string]*websocket.Conn
}

func NewManager(mc *memcache.Client, rdb *redis.Client, stream stream.Service, util util.Config) Manager {
	newmanager := &manager{
		mc: mc,
		rdb: rdb,
		stream: stream,
		config: util,
		clientSubs: make(map[*websocket.Conn][]string),
		remoteSubs: make(map[string][]string),
		remoteConns: make(map[string]*websocket.Conn),
	}
	go newmanager.chunkUpdaterRoutine()
	return newmanager
}


func NewSubscriptionManagerForTest(mc *memcache.Client, rdb *redis.Client, stream stream.Service) *manager {
	manager := &manager{
		mc: mc,
		rdb: rdb,
		stream: stream,
		clientSubs: make(map[*websocket.Conn][]string),
		remoteSubs: make(map[string][]string),
		remoteConns: make(map[string]*websocket.Conn),
	}
	return manager
}

// Subscribe subscribes a client to a stream
func (m *manager) Subscribe(conn *websocket.Conn, streams []string) {
	m.clientSubs[conn] = streams
	m.createInsufficientSubs() // TODO: this should be done in a goroutine
}

// Unsubscribe unsubscribes a client from a stream
func (m *manager) Unsubscribe(conn *websocket.Conn) {
	delete(m.clientSubs, conn)
}

func (m *manager) createInsufficientSubs() {
	currentSubs := make(map[string][]string)
	for _, streams := range m.clientSubs {
		for _, stream := range streams {
			split := strings.Split(stream, "@")
			if len(split) != 2 {
				continue
			}
			domain := split[1]
			if _, ok := currentSubs[domain]; !ok {
				currentSubs[domain] = append(currentSubs[domain], stream)
			}
		}
	}

	// on this func, update only if there is a new subscription
	changedRemotes := make([]string, 0)
	for domain, streams := range currentSubs {
		if domain == m.config.Concurrent.FQDN {
			continue
		}
		if _, ok := m.remoteSubs[domain]; !ok {
			m.remoteSubs[domain] = streams
			changedRemotes = append(changedRemotes, domain)
		} else {
			for _, stream := range streams {
				if !slices.Contains(m.remoteSubs[domain], stream) {
					m.remoteSubs[domain] = append(m.remoteSubs[domain], stream)
					changedRemotes = append(changedRemotes, domain)
				}
			}
		}
	}

	log.Printf("remote subscription created: %v", m.remoteSubs)

	for _, domain := range changedRemotes {
		m.RemoteSubRoutine(domain, m.remoteSubs[domain])
	}
}

// DeleteExcessiveSubs deletes subscriptions that are not needed anymore
func (m *manager) deleteExcessiveSubs() {
	var currentSubs []string
	for _, streams := range m.clientSubs {
		for _, stream := range streams {
			if !slices.Contains(currentSubs, stream) {
				currentSubs = append(currentSubs, stream)
			}
		}
	}

	var closeList []string

	for domain, streams := range m.remoteSubs {
		for _, stream := range streams {
			var newSubs []string
			for _, currentSub := range currentSubs {
				if currentSub == stream {
					newSubs = append(newSubs, currentSub)
				}
			}
			m.remoteSubs[domain] = newSubs

			if len(m.remoteSubs[domain]) == 0 {
				closeList = append(closeList, domain)
			}
		}
	}

	for _, domain := range closeList {

		// close connection
		if conn, ok := m.remoteConns[domain]; ok {
			conn.Close()
		}

		delete(m.remoteSubs, domain)
		delete(m.remoteConns, domain)
	}

	log.Printf("remote subscription cleaned up: %v", closeList)
}

// RemoteSubRoutine subscribes to a remote server
func (m *manager) RemoteSubRoutine(domain string, streams []string) {
	if _, ok := m.remoteConns[domain]; !ok {
		// new server, create new connection
		u := url.URL{Scheme: "wss", Host: domain, Path: "/api/v1/socket"}
		dialer := websocket.DefaultDialer
		dialer.HandshakeTimeout = 10 * time.Second

		// TODO: add config for TLS
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

		c, _, err := dialer.Dial(u.String(), nil)
		if err != nil {
			log.Printf("[remote websocket] fail to dial: %v", err)
			delete(m.remoteConns, domain)
			return
		}

		m.remoteConns[domain] = c

		messageChan := make(chan []byte)
		// goroutine for reading messages from remote server
		go func(c *websocket.Conn, messageChan chan<- []byte) {
			defer func() {
				if c != nil {
					c.Close()
				}
				delete(m.remoteConns, domain)
				log.Printf("##### remote connection closed: %s", domain)
			}()
			for {
				// check if the connection is still alive
				if c == nil {
					log.Printf("connection is nil (domain: %s)", domain)
					break
				}
				_, message, err := c.ReadMessage()
				if err != nil {
					log.Printf("fail to read message: %v", err)
					break
				}
				messageChan <- message
			}
		}(c, messageChan)

		// goroutine for relay messages to clients
		go func(c *websocket.Conn, messageChan <-chan []byte) {
			pingTicker := time.NewTicker(pingInterval)
			defer func() {
				if c != nil {
					c.Close()
				}
				pingTicker.Stop()
				delete(m.remoteConns, domain)
				log.Printf("##### remote connection closed: %s", domain)
			}()

			var lastPong time.Time = time.Now()
			c.SetPongHandler(func(string) error {
				lastPong = time.Now()
				return nil
			})

			for {
				select {
					case message := <-messageChan:

						log.Printf("[remote] <- %s\n", message[:64])

						var event stream.Event
						err = json.Unmarshal(message, &event)
						if err != nil {
							log.Printf("fail to Unmarshall redis message: %v", err)
							continue
						}

						// publish message to Redis
						err = m.rdb.Publish(ctx, event.Stream, string(message)).Err()
						if err != nil {
							log.Printf("fail to publish message to Redis: %v", err)
							continue
						}

						// update cache
						json, err := json.Marshal(event.Item)
						if err != nil {
							log.Printf("fail to Marshall item: %v", err)
							continue
						}
						json = append(json, ',')

						// update cache
						cacheKey := "stream:body:all:" + event.Item.StreamID + ":" + stream.Time2Chunk(event.Item.CDate)
						err = m.mc.Append(&memcache.Item{Key: cacheKey, Value: json})
						if err != nil {
							// キャッシュがなかった場合、リモートからチャンクを取得し直す
							chunks, err := m.stream.GetChunksFromRemote(ctx, domain, []string{event.Item.StreamID}, event.Item.CDate)
							if err != nil {
								log.Printf("fail to get chunks from remote: %v", err)
								continue
							}

							if stream.Time2Chunk(event.Item.CDate) != stream.Time2Chunk(time.Now()) {
								log.Println("remote-sent chunk is not the latest")
								continue
							}

							if chunk, ok := chunks[event.Item.StreamID]; ok {
								key := "stream:itr:all:" + event.Item.StreamID + ":" + stream.Time2Chunk(time.Now())
								m.mc.Set(&memcache.Item{Key: key, Value: []byte(chunk.Key)})
							}
						}
					case <-pingTicker.C:
						if err := c.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
							log.Printf("fail to send ping message: %v", err)
							return
						}
						if lastPong.Before(time.Now().Add(-disconnectTimeout)) {
							log.Printf("pong timeout: %s", domain)
							return
						}
				}
			}
		}(c, messageChan)
	}
	request := channelRequest{
		Channels: streams,
	}
	err := m.remoteConns[domain].WriteJSON(request)
	if err != nil {
		log.Printf("fail to send subscribe request to remote server %v: %v", domain, err)
		delete(m.remoteConns, domain)
		return
	}
	log.Printf("[remote] connection updated: %s > %s", domain, streams)
}

/*
func (m *manager) updateChunks(newchunk string) {
	// update cache
	for _, streams := range m.remoteSubs {
		for _, stream := range streams {
			m.mc.Add(&memcache.Item{Key: "stream:body:all:" + stream + ":" + newchunk, Value: []byte("")})
		}
	}
}
*/

// ChunkUpdaterRoutine
func (m *manager) chunkUpdaterRoutine() {
	currentChunk := stream.Time2Chunk(time.Now())
	for {
		// 次の実行時刻を計算
		nextRun := time.Now().Truncate(time.Hour).Add(time.Minute * 10)
		if time.Now().After(nextRun) {
			// 現在時刻がnextRunを過ぎている場合、次の10分単位の時刻を計算
			elapsed := time.Now().Sub(nextRun)
			nextRun = nextRun.Add(time.Minute * 10 * ((elapsed / (time.Minute * 10)) + 1))
		}

		// 次の実行時刻まで待機
		time.Sleep(time.Until(nextRun))

		// まだだったら待ちなおす
		newChunk := stream.Time2Chunk(time.Now())
		if newChunk == currentChunk {
			continue
		}

		log.Printf("update chunks: %s -> %s", currentChunk, newChunk)

		m.deleteExcessiveSubs()
		//m.updateChunks(newChunk)

		currentChunk = newChunk
	}
}

type channelRequest struct {
	Channels []string `json:"channels"`
}

