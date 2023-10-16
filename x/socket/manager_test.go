package socket

import (
	//"context"
	"log"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/totegamma/concurrent/internal/testutil"

	//"github.com/totegamma/concurrent/x/core"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"

	"fmt"
	"strings"
	"sync/atomic"
	"net/http"
	"net/http/httptest"
)

// var ctx = context.Background()
var mc *memcache.Client
var rdb *redis.Client
var pivot time.Time
var m *subscriptionManager

func TestMain(tm *testing.M) {
	log.Println("Test Start")

	var cleanup_rdb func()
	rdb, cleanup_rdb = testutil.CreateRDB()
	defer cleanup_rdb()

	var cleanup_mc func()
	mc, cleanup_mc = testutil.CreateMC()
	defer cleanup_mc()

	m = NewSubscriptionManager(mc, rdb)
	pivot = time.Now()
	tm.Run()

	log.Println("Test End")
}

type wsHandler struct{}
var latestClientID = atomic.Int64{}
func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := latestClientID.Add(1)
	mylog := log.Default()
	mylog.SetPrefix(fmt.Sprintf("client%d: ", clientID))

	mylog.Println("connected")
	defer mylog.Println("disconnected")

	c, err := new(websocket.Upgrader).Upgrade(w, r, nil)
	if err != nil {
		mylog.Println("upgrade error:", err)
		return
	}
	defer c.Close()

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			mylog.Println("read error:", err)
			break
		}
		log.Println(string(msg))

		/*
		if err := c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("You said: %s", msg))); err != nil {
			mylog.Println("write error:", err)
			break
		}
		*/
	}
}


func TestManager(t *testing.T) {

	// ローカルのサブスクリプションではリモートsubが作成されないことを確認
	conn0 := websocket.Conn{}
	m.Subscribe(&conn0, []string{"local0", "local1"})
	assert.Len(t, m.clientSubs, 1)
	assert.Len(t, m.clientSubs[&conn0], 2)
	assert.Len(t, m.remoteSubs, 0)

	server := httptest.NewTLSServer(&wsHandler{})
	server.URL = strings.Replace(server.URL, "https", "wss", 1)
	domain := strings.Replace(server.URL, "wss://", "", 1)

	// リモートのサブスクリプションでリモートsubが作成されることを確認
	remotestream0 := "remote0@" + domain
	conn1 := websocket.Conn{}
	m.Subscribe(&conn1, []string{remotestream0})
	assert.Len(t, m.clientSubs, 2) // 2つ目のサブスクリプション
	assert.Len(t, m.clientSubs[&conn1], 1)
	assert.Len(t, m.remoteSubs, 1)
	assert.Len(t, m.remoteSubs[domain], 1)

	// リモートサブスクリプションを更新するが、減らないことを確認

	// 外からメッセージを流して、キャッシュが更新されることを確認

	// - キャッシュが存在するとき

	// - キャッシュが存在しないとき

	// 手動でchunkupdaterroutineを呼び出して、キャッシュが意図通りに更新されることを確認
}

