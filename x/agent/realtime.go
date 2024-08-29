package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gorilla/websocket"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel/attribute"
)

var (
	pingInterval      = 10 * time.Second
	disconnectTimeout = 30 * time.Second
	remoteSubs        = make(map[string][]string)
	remoteConns       = make(map[string]*websocket.Conn)
)

type channelRequest struct {
	Type     string   `json:"type"`
	Channels []string `json:"channels"`
}

func (a *agent) GetCurrentSubs(ctx context.Context) []string {

	query := a.rdb.PubSubChannels(ctx, "*")
	channels := query.Val()

	uniqueChannelsMap := make(map[string]bool)
	for _, channel := range channels {
		uniqueChannelsMap[channel] = true
	}

	uniqueChannels := make([]string, 0)
	for channel := range uniqueChannelsMap {
		split := strings.Split(channel, "@")
		if len(split) <= 1 {
			continue
		}
		uniqueChannels = append(uniqueChannels, channel)
	}

	return uniqueChannels
}

// update m.remoteSubs
// also update remoteConns if needed
func (a *agent) createInsufficientSubs(ctx context.Context) {

	currentSubs := a.GetCurrentSubs(ctx)

	// update remoteSubs
	// only add new subscriptions
	// also detect remote subscription changes
	changedRemotes := make([]string, 0)
	for _, timeline := range currentSubs {
		split := strings.Split(timeline, "@")
		if len(split) <= 1 {
			continue
		}
		domain := split[len(split)-1]

		if domain == a.config.FQDN {
			continue
		}

		if _, ok := remoteSubs[domain]; !ok {
			remoteSubs[domain] = []string{timeline}
			if !slices.Contains(changedRemotes, domain) {
				changedRemotes = append(changedRemotes, domain)
			}
		} else {
			if !slices.Contains(remoteSubs[domain], timeline) {
				remoteSubs[domain] = append(remoteSubs[domain], timeline)
				if !slices.Contains(changedRemotes, domain) {
					changedRemotes = append(changedRemotes, domain)
				}
			}
		}
	}

	for _, domain := range changedRemotes {
		a.RemoteSubRoutine(ctx, domain, remoteSubs[domain])
	}
}

// DeleteExcessiveSubs deletes subscriptions that are not needed anymore
func (a *agent) deleteExcessiveSubs(ctx context.Context) {

	currentSubs := a.GetCurrentSubs(ctx)

	var closeList []string

	for domain, timelines := range remoteSubs {
		for _, timeline := range timelines {
			var newSubs []string
			for _, currentSub := range currentSubs {
				if currentSub == timeline {
					newSubs = append(newSubs, currentSub)
				}
			}
			remoteSubs[domain] = newSubs

			if len(remoteSubs[domain]) == 0 {
				closeList = append(closeList, domain)
			}
		}
	}

	for _, domain := range closeList {

		// close connection
		if conn, ok := remoteConns[domain]; ok {
			conn.Close()
		}

		delete(remoteSubs, domain)
		delete(remoteConns, domain)
	}

	slog.Info(
		fmt.Sprintf("subscription cleaned up: %v", closeList),
		slog.String("module", "agent"),
		slog.String("group", "realtime"),
	)
}

// RemoteSubRoutine subscribes to a remote server
func (a *agent) RemoteSubRoutine(ctx context.Context, domain string, timelines []string) {
	if _, ok := remoteConns[domain]; !ok {
		// new server, create new connection

		// check server availability
		domainInfo, err := a.client.GetDomain(ctx, domain, nil)
		if err != nil {
			slog.Error(
				fmt.Sprintf("fail to get domain info: %v", err),
				slog.String("module", "agent"),
				slog.String("group", "realtime"),
			)
			return
		}
		if domainInfo.Dimension != a.config.Dimension {
			slog.Error(
				fmt.Sprintf("domain dimention mismatch: %s", domain),
				slog.String("module", "agent"),
				slog.String("group", "realtime"),
			)
			return
		}

		u := url.URL{Scheme: "wss", Host: domain, Path: "/api/v1/timelines/realtime"}
		dialer := websocket.DefaultDialer
		dialer.HandshakeTimeout = 10 * time.Second

		c, _, err := dialer.Dial(u.String(), nil)
		if err != nil {
			slog.Error(
				fmt.Sprintf("fail to dial to %v (%v)", domain, err),
				slog.String("module", "agent"),
				slog.String("group", "realtime"),
			)

			delete(remoteConns, domain)
			return
		}

		remoteConns[domain] = c

		messageChan := make(chan []byte)
		// goroutine for reading messages from remote server
		go func(c *websocket.Conn, messageChan chan<- []byte) {
			defer func() {
				if c != nil {
					c.Close()
				}
				delete(remoteConns, domain)
				slog.Info(
					fmt.Sprintf("remote connection closed: %s", domain),
					slog.String("module", "agent"),
					slog.String("group", "realtime"),
				)
			}()
			for {
				// check if the connection is still alive
				if c == nil {
					slog.Info(
						fmt.Sprintf("connection is nil (domain: %s)", domain),
						slog.String("module", "agent"),
						slog.String("group", "realtime"),
					)
					break
				}
				_, message, err := c.ReadMessage()
				if err != nil {
					slog.Error(
						fmt.Sprintf("fail to read message: %v", err),
						slog.String("module", "agent"),
						slog.String("group", "realtime"),
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
				delete(remoteConns, domain)
				slog.Info(
					fmt.Sprintf("remote connection closed: %s", domain),
					slog.String("module", "agent"),
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
						slog.String("module", "agent"),
						slog.String("group", "realtime"),
					)

					var event core.Event
					err = json.Unmarshal(message, &event)
					if err != nil {
						slog.Error(
							fmt.Sprintf("fail to Unmarshall redis message"),
							slog.String("error", err.Error()),
							slog.String("module", "agent"),
							slog.String("group", "realtime"),
						)
						continue
					}

					// publish message to Redis
					err = a.rdb.Publish(ctx, event.Timeline, string(message)).Err()
					if err != nil {
						slog.Error(
							fmt.Sprintf("fail to publish message to Redis"),
							slog.String("error", err.Error()),
							slog.String("module", "agent"),
							slog.String("group", "realtime"),
						)
						continue
					}

					// update cache
					json, err := json.Marshal(event.Item)
					if err != nil {
						slog.Error(
							"fail to Marshall item",
							slog.String("error", err.Error()),
							slog.String("module", "agent"),
							slog.String("group", "realtime"),
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
					itr := "tl:itr:" + timelineID + ":" + core.Time2Chunk(event.Item.CDate)
					itrVal, err := a.mc.Get(itr)
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
							slog.String("module", "agent"),
							slog.String("group", "realtime"),
						)
						continue
					}

					err = a.mc.Append(&memcache.Item{Key: cacheKey, Value: json})
					if err != nil {
						slog.Error(
							fmt.Sprintf("fail to update cache: %s", itr),
							slog.String("error", err.Error()),
							slog.String("module", "agent"),
							slog.String("group", "realtime"),
						)
					}

				case <-pingTicker.C:
					if err := c.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
						slog.Error(
							fmt.Sprintf("fail to send ping message: %v", err),
							slog.String("module", "agent"),
							slog.String("group", "realtime"),
						)
						return
					}
					if lastPong.Before(time.Now().Add(-disconnectTimeout)) {
						slog.Warn(
							fmt.Sprintf("pong timeout: %s", domain),
							slog.String("module", "agent"),
							slog.String("group", "realtime"),
						)
						return
					}
				}
			}
		}(c, messageChan)
	}
	request := channelRequest{
		Type:     "listen",
		Channels: timelines,
	}
	err := remoteConns[domain].WriteJSON(request)
	if err != nil {
		slog.Error(
			fmt.Sprintf("fail to send subscribe request to remote server %v", domain),
			slog.String("error", err.Error()),
			slog.String("module", "agent"),
			slog.String("group", "realtime"),
		)

		delete(remoteConns, domain)
		return
	}
	slog.Info(
		fmt.Sprintf("remote connection updated: %s > %s", domain, timelines),
		slog.String("module", "agent"),
		slog.String("group", "realtime"),
	)
}

// ConnectionKeeperRoutine
// 接続が失われている場合、再接続を試みる
func (a *agent) connectionKeeperRoutine(ctx context.Context) {

	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.createInsufficientSubs(ctx)
			slog.InfoContext(
				ctx,
				fmt.Sprintf("connection keeper: %d/%d", len(remoteConns), len(remoteSubs)),
				slog.String("module", "agent"),
				slog.String("group", "realtime"),
			)
			for domain := range remoteSubs {
				if _, ok := remoteConns[domain]; !ok {
					slog.Info(
						fmt.Sprintf("broken connection found: %s", domain),
						slog.String("module", "agent"),
						slog.String("group", "realtime"),
					)
					a.RemoteSubRoutine(ctx, domain, remoteSubs[domain])
				}
			}
		}
	}
}

// ChunkUpdaterRoutine
func (a *agent) chunkUpdaterRoutine(ctx context.Context) {
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

		ctx, span := tracer.Start(ctx, "Agent.chunkUpdaterRoutine")
		defer span.End()

		span.SetAttributes(attribute.String("currentChunk", currentChunk))

		slog.Info(
			fmt.Sprintf("update chunks: %s -> %s", currentChunk, newChunk),
			slog.String("module", "agent"),
			slog.String("group", "realtime"),
		)

		a.deleteExcessiveSubs(ctx)

		currentChunk = newChunk
	}
}

// watchEventRoutine
func (a *agent) watchEventRoutine(ctx context.Context) {

	pubsub := a.rdb.Subscribe(ctx, "concrnt:subscription:updated")
	defer pubsub.Close()

	psch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-psch:
			if msg == nil {
				slog.Warn(
					"received nil message",
					slog.String("module", "agent"),
					slog.String("group", "realtime"),
				)
				continue
			}

			slog.Info(
				fmt.Sprintf("received message: %s", msg.Payload),
				slog.String("module", "agent"),
				slog.String("group", "realtime"),
			)
			a.createInsufficientSubs(ctx)
		}
	}

}
