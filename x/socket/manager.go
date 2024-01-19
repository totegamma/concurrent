//go:generate go run go.uber.org/mock/mockgen -source=manager.go -destination=mock/manager.go

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

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
)

// ソケット管理のルール
// 1. Subscribeのリクエストのうち外部のリクエストは増える方向にしか更新をかけない
// 2. 外から受信したイベントのうち、すでにキャッシュのキーがあるものに対してappendをかける
// 3. chunk更新タイミングで、外部リクエストの棚卸しを行う

var ctx = context.Background()

var (
	pingInterval      = 10 * time.Second
	disconnectTimeout = 30 * time.Second
)

type Manager interface {
	Subscribe(conn *websocket.Conn, streams []string)
	Unsubscribe(conn *websocket.Conn)
	GetAllRemoteSubs() []string
}

type manager struct {
	mc     *memcache.Client
	rdb    *redis.Client
	config util.Config

	clientSubs  map[*websocket.Conn][]string
	remoteSubs  map[string][]string
	remoteConns map[string]*websocket.Conn
}

func NewManager(mc *memcache.Client, rdb *redis.Client, util util.Config) Manager {
	newmanager := &manager{
		mc:          mc,
		rdb:         rdb,
		config:      util,
		clientSubs:  make(map[*websocket.Conn][]string),
		remoteSubs:  make(map[string][]string),
		remoteConns: make(map[string]*websocket.Conn),
	}
	go newmanager.chunkUpdaterRoutine()
	go newmanager.connectionKeeperRoutine()
	return newmanager
}

func NewSubscriptionManagerForTest(mc *memcache.Client, rdb *redis.Client) *manager {
	manager := &manager{
		mc:          mc,
		rdb:         rdb,
		clientSubs:  make(map[*websocket.Conn][]string),
		remoteSubs:  make(map[string][]string),
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
	if _, ok := m.clientSubs[conn]; ok {
		delete(m.clientSubs, conn)
	}
}

// GetAllRemoteSubs returns all remote subscriptions
func (m *manager) GetAllRemoteSubs() []string {

	allSubsMap := make(map[string]bool)
	for _, subs := range m.remoteSubs {
		for _, sub := range subs {
			allSubsMap[sub] = true
		}
	}

	allSubs := make([]string, 0)
	for sub := range allSubsMap {
		allSubs = append(allSubs, sub)
	}

	return allSubs
}

// update m.remoteSubs
// also update remoteConns if needed
func (m *manager) createInsufficientSubs() {
	currentSubs := make(map[string]bool)
	for _, streams := range m.clientSubs {
		for _, stream := range streams {
			currentSubs[stream] = true
		}
	}

	// update remoteSubs
	// only add new subscriptions
	// also detect remote subscription changes
	changedRemotes := make([]string, 0)
	for stream := range currentSubs {
		split := strings.Split(stream, "@")
		if len(split) != 2 {
			continue
		}
		domain := split[1]

		if domain == m.config.Concurrent.FQDN {
			continue
		}

		if _, ok := m.remoteSubs[domain]; !ok {
			m.remoteSubs[domain] = []string{stream}
			if !slices.Contains(changedRemotes, domain) {
				changedRemotes = append(changedRemotes, domain)
			}
		} else {
			if !slices.Contains(m.remoteSubs[domain], stream) {
				m.remoteSubs[domain] = append(m.remoteSubs[domain], stream)
				if !slices.Contains(changedRemotes, domain) {
					changedRemotes = append(changedRemotes, domain)
				}
			}
		}
	}

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

	log.Printf("[remote] subscription cleaned up: %v", closeList)
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
				log.Printf("[remote ws.reader] remote connection closed: %s", domain)
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
				log.Printf("[remote ws.publisher] remote connection closed: %s", domain)
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

					var event core.Event
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

					streamID := event.Item.StreamID
					if !strings.Contains(streamID, "@") {
						streamID = streamID + "@" + domain
					}

					// update cache
					// first, try to get itr
					itr := "stream:itr:all:" + streamID + ":" + core.Time2Chunk(event.Item.CDate)
					itrVal, err := m.mc.Get(itr)
					var cacheKey string
					if err == nil {
						cacheKey = string(itrVal.Value)
					} else {
						// 最新時刻のイテレーターがないということは、キャッシュがないということ
						// とはいえ今後はいい感じにキャッシュを作れるようにしたい
						// 例えば、今までのキャッシュを(現時点では取得不能)最新のitrが指すようにして
						// 今までのキャッシュを更新し続けるとか... (TODO)
						// cacheKey := "stream:body:all:" + event.Item.StreamID + ":" + core.Time2Chunk(event.Item.CDate)
						log.Printf("[remote] no need to update cache: %s", itr)
						continue
					}

					err = m.mc.Append(&memcache.Item{Key: cacheKey, Value: json})
					if err != nil {
						log.Printf("fail to update cache: %v", err)
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
		log.Printf("[remote] fail to send subscribe request to remote server %v: %v", domain, err)
		delete(m.remoteConns, domain)
		return
	}
	log.Printf("[remote] connection updated: %s > %s", domain, streams)
}

// ConnectionKeeperRoutine
// 接続が失われている場合、再接続を試みる
func (m *manager) connectionKeeperRoutine() {

	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Printf("[remote] connection keeper: %d/%d\n", len(m.remoteSubs), len(m.remoteConns))
			for domain := range m.remoteSubs {
				if _, ok := m.remoteConns[domain]; !ok {
					log.Printf("[remote] broken connection found: %s\n", domain)
					m.RemoteSubRoutine(domain, m.remoteSubs[domain])
				}
			}
		}
	}
}

// ChunkUpdaterRoutine
func (m *manager) chunkUpdaterRoutine() {
	currentChunk := core.Time2Chunk(time.Now())
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
		newChunk := core.Time2Chunk(time.Now())
		if newChunk == currentChunk {
			continue
		}

		log.Printf("[manager] update chunks: %s -> %s", currentChunk, newChunk)

		m.deleteExcessiveSubs()
		//m.updateChunks(newChunk)

		currentChunk = newChunk
	}
}

type channelRequest struct {
	Channels []string `json:"channels"`
}
