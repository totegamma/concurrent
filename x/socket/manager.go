package socket

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
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

type manager struct {
	mc     *memcache.Client
	rdb    *redis.Client
	config util.Config

	clientSubs  map[*websocket.Conn][]string
	remoteSubs  map[string][]string
	remoteConns map[string]*websocket.Conn
}

func NewManager(mc *memcache.Client, rdb *redis.Client, util util.Config) core.SocketManager {
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

// Subscribe subscribes a client to a timeline
func (m *manager) Subscribe(conn *websocket.Conn, timelines []string) {
	m.clientSubs[conn] = timelines
	m.createInsufficientSubs() // TODO: this should be done in a goroutine
}

// Unsubscribe unsubscribes a client from a timeline
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
	for _, timelines := range m.clientSubs {
		for _, timeline := range timelines {
			currentSubs[timeline] = true
		}
	}

	// update remoteSubs
	// only add new subscriptions
	// also detect remote subscription changes
	changedRemotes := make([]string, 0)
	for timeline := range currentSubs {
		split := strings.Split(timeline, "@")
		if len(split) != 2 {
			continue
		}
		domain := split[1]

		if domain == m.config.Concurrent.FQDN {
			continue
		}

		if _, ok := m.remoteSubs[domain]; !ok {
			m.remoteSubs[domain] = []string{timeline}
			if !slices.Contains(changedRemotes, domain) {
				changedRemotes = append(changedRemotes, domain)
			}
		} else {
			if !slices.Contains(m.remoteSubs[domain], timeline) {
				m.remoteSubs[domain] = append(m.remoteSubs[domain], timeline)
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
	for _, timelines := range m.clientSubs {
		for _, timeline := range timelines {
			if !slices.Contains(currentSubs, timeline) {
				currentSubs = append(currentSubs, timeline)
			}
		}
	}

	var closeList []string

	for domain, timelines := range m.remoteSubs {
		for _, timeline := range timelines {
			var newSubs []string
			for _, currentSub := range currentSubs {
				if currentSub == timeline {
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

	slog.Info(
		fmt.Sprintf("subscription cleaned up: %v", closeList),
		slog.String("module", "socket"),
		slog.String("group", "remote"),
	)
}

// RemoteSubRoutine subscribes to a remote server
func (m *manager) RemoteSubRoutine(domain string, timelines []string) {
	if _, ok := m.remoteConns[domain]; !ok {
		// new server, create new connection
		u := url.URL{Scheme: "wss", Host: domain, Path: "/api/v1/socket"}
		dialer := websocket.DefaultDialer
		dialer.HandshakeTimeout = 10 * time.Second

		// TODO: add config for TLS
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

		c, _, err := dialer.Dial(u.String(), nil)
		if err != nil {
			slog.Error(
				fmt.Sprintf("fail to dial: %v", err),
				slog.String("module", "socket"),
				slog.String("group", "remote"),
			)

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
				slog.Info(
					fmt.Sprintf("remote connection closed: %s", domain),
					slog.String("module", "socket"),
					slog.String("group", "remote"),
				)
			}()
			for {
				// check if the connection is still alive
				if c == nil {
					slog.Info(
						fmt.Sprintf("connection is nil (domain: %s)", domain),
						slog.String("module", "socket"),
						slog.String("group", "remote"),
					)
					break
				}
				_, message, err := c.ReadMessage()
				if err != nil {
					slog.Error(
						fmt.Sprintf("fail to read message: %v", err),
						slog.String("module", "socket"),
						slog.String("group", "remote"),
					)
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
				slog.Info(
					fmt.Sprintf("remote connection closed: %s", domain),
					slog.String("module", "socket"),
					slog.String("group", "remote ws.publisher"),
				)
			}()

			var lastPong time.Time = time.Now()
			c.SetPongHandler(func(string) error {
				lastPong = time.Now()
				return nil
			})

			for {
				select {
				case message := <-messageChan:

					slog.Debug(
						fmt.Sprintf("remote message received: %s", message[:64]),
						slog.String("module", "socket"),
						slog.String("group", "remote"),
					)

					var event core.Event
					err = json.Unmarshal(message, &event)
					if err != nil {
						slog.Error(
							fmt.Sprintf("fail to Unmarshall redis message"),
							slog.String("error", err.Error()),
							slog.String("module", "socket"),
							slog.String("group", "remote"),
						)
						continue
					}

					// publish message to Redis
					err = m.rdb.Publish(ctx, event.Timeline, string(message)).Err()
					if err != nil {
						slog.Error(
							fmt.Sprintf("fail to publish message to Redis"),
							slog.String("error", err.Error()),
							slog.String("module", "socket"),
							slog.String("group", "remote"),
						)
						continue
					}

					// update cache
					json, err := json.Marshal(event.Item)
					if err != nil {
						slog.Error(
							"fail to Marshall item",
							slog.String("error", err.Error()),
							slog.String("module", "socket"),
							slog.String("group", "remote"),
						)
						continue
					}
					json = append(json, ',')

					timelineID := event.Item.TimelineID
					if !strings.Contains(timelineID, "@") {
						timelineID = timelineID + "@" + domain
					}

					// update cache
					// first, try to get itr
					itr := "timeline:itr:all:" + timelineID + ":" + core.Time2Chunk(event.Item.CDate)
					itrVal, err := m.mc.Get(itr)
					var cacheKey string
					if err == nil {
						cacheKey = string(itrVal.Value)
					} else {
						// 最新時刻のイテレーターがないということは、キャッシュがないということ
						// とはいえ今後はいい感じにキャッシュを作れるようにしたい
						// 例えば、今までのキャッシュを(現時点では取得不能)最新のitrが指すようにして
						// 今までのキャッシュを更新し続けるとか... (TODO)
						// cacheKey := "timeline:body:all:" + event.Item.TimelienID + ":" + core.Time2Chunk(event.Item.CDate)
						slog.Info(
							fmt.Sprintf("no need to update cache: %s", itr),
							slog.String("module", "socket"),
							slog.String("group", "remote"),
						)
						continue
					}

					err = m.mc.Append(&memcache.Item{Key: cacheKey, Value: json})
					if err != nil {
						slog.Error(
							fmt.Sprintf("fail to update cache: %s", itr),
							slog.String("error", err.Error()),
							slog.String("module", "socket"),
							slog.String("group", "remote"),
						)
					}

				case <-pingTicker.C:
					if err := c.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
						slog.Error(
							fmt.Sprintf("fail to send ping message: %v", err),
							slog.String("module", "socket"),
							slog.String("group", "remote"),
						)
						return
					}
					if lastPong.Before(time.Now().Add(-disconnectTimeout)) {
						slog.Warn(
							fmt.Sprintf("pong timeout: %s", domain),
							slog.String("module", "socket"),
							slog.String("group", "remote"),
						)
						return
					}
				}
			}
		}(c, messageChan)
	}
	request := channelRequest{
		Channels: timelines,
	}
	err := m.remoteConns[domain].WriteJSON(request)
	if err != nil {
		slog.Error(
			fmt.Sprintf("fail to send subscribe request to remote server %v", domain),
			slog.String("error", err.Error()),
			slog.String("module", "socket"),
			slog.String("group", "remote"),
		)

		delete(m.remoteConns, domain)
		return
	}
	slog.Info(
		fmt.Sprintf("remote connection updated: %s > %s", domain, timelines),
		slog.String("module", "socket"),
		slog.String("group", "remote"),
	)
}

// ConnectionKeeperRoutine
// 接続が失われている場合、再接続を試みる
func (m *manager) connectionKeeperRoutine() {

	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			slog.InfoContext(
				ctx,
				fmt.Sprintf("connection keeper: %d/%d", len(m.remoteSubs), len(m.remoteConns)),
				slog.String("module", "socket"),
				slog.String("group", "remote"),
			)
			for domain := range m.remoteSubs {
				if _, ok := m.remoteConns[domain]; !ok {
					slog.Info(
						fmt.Sprintf("broken connection found: %s", domain),
						slog.String("module", "socket"),
						slog.String("group", "remote"),
					)
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

		slog.Info(
			fmt.Sprintf("update chunks: %s -> %s", currentChunk, newChunk),
			slog.String("module", "socket"),
			slog.String("group", "remote"),
		)

		m.deleteExcessiveSubs()
		//m.updateChunks(newChunk)

		currentChunk = newChunk
	}
}

type channelRequest struct {
	Channels []string `json:"channels"`
}
