package socket

import (
	//"context"
	"log"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/totegamma/concurrent/internal/testutil"

	"github.com/totegamma/concurrent/x/core"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"

	"go.uber.org/mock/gomock"

	"fmt"
	"strings"
	"sync/atomic"
	"net/http"
	"net/http/httptest"
	"encoding/json"
)

// var ctx = context.Background()
var mc *memcache.Client
var rdb *redis.Client
var pivot time.Time

func TestMain(tm *testing.M) {
	log.Println("Test Start")

	var cleanup_rdb func()
	rdb, cleanup_rdb = testutil.CreateRDB()
	defer cleanup_rdb()

	var cleanup_mc func()
	mc, cleanup_mc = testutil.CreateMC()
	defer cleanup_mc()

	pivot = time.Now()
	tm.Run()

	log.Println("Test End")
}

type wsHandler struct {
	conn *websocket.Conn
}
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

	h.conn = c

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			mylog.Println("read error:", err)
			break
		}
		log.Println(string(msg))
	}
}

func (h *wsHandler) EmitMessage(msg []byte) {
	if h.conn == nil {
		log.Fatal("conn is nil")
		return
	}
	if err := h.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Println("write error:", err)
	}
}

func TestManager(t *testing.T) {

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m := NewSubscriptionManagerForTest(mc, rdb)

	// ローカルのサブスクリプションではリモートsubが作成されないことを確認
	conn0 := websocket.Conn{}
	m.Subscribe(&conn0, []string{"local0", "local1"})
	assert.Len(t, m.clientSubs, 1)
	assert.Len(t, m.clientSubs[&conn0], 2)
	assert.Len(t, m.remoteSubs, 0)

	// サブスクリプションが切り替わることを確認
	m.Subscribe(&conn0, []string{"local2"})
	assert.Len(t, m.clientSubs, 1)
	assert.Len(t, m.clientSubs[&conn0], 1)
	assert.Equal(t, m.clientSubs[&conn0][0], "local2")
	assert.Len(t, m.remoteSubs, 0)

	wshandler := &wsHandler{}
	server := httptest.NewTLSServer(wshandler)
	server.URL = strings.Replace(server.URL, "https", "wss", 1)
	domain := strings.Replace(server.URL, "wss://", "", 1)

	// リモートのサブスクリプションでリモートsubが作成されることを確認
	remotestream0 := "remote0@" + domain
	conn1 := websocket.Conn{}
	m.Subscribe(&conn1, []string{remotestream0})
	time.Sleep(1 * time.Second)
	assert.Len(t, m.clientSubs, 2) // 2つ目のサブスクリプション
	assert.Len(t, m.clientSubs[&conn1], 1)
	assert.Len(t, m.remoteSubs, 1)
	assert.Len(t, m.remoteSubs[domain], 1)

	// リモートサブスクリプションを更新するが、減らないことを確認
	remotestream1 := "remote1@" + domain
	m.Subscribe(&conn1, []string{remotestream1})
	time.Sleep(1 * time.Second)
	assert.Len(t, m.clientSubs, 2)
	assert.Len(t, m.clientSubs[&conn1], 1)
	assert.Len(t, m.remoteSubs, 1)
	assert.Len(t, m.remoteSubs[domain], 2) // 普通に増える

	// 外からメッセージを流して、キャッシュが更新されることを確認

	key := "stream:body:all:" + remotestream1 + ":" + core.Time2Chunk(pivot)

	// - キャッシュが存在しないとき
	testEvent := core.Event{
		Stream: remotestream1,
		Action: "create",
		Type:   "message",
		Item:   core.StreamItem{
			Type: "message",
			ObjectID: "",
			StreamID: remotestream1,
			Owner: "",
			Author: "",
			CDate: pivot,
		},
		Body:  []byte(`{"foo":"bar"}`),
	}
	jsonstr, _ := json.Marshal(testEvent)
	wshandler.EmitMessage(jsonstr)

	time.Sleep(1 * time.Second)

	_, err := mc.Get(key)
	assert.Error(t, err)

	// - キャッシュが存在するとき
	mc.Set(&memcache.Item{Key: key,Value: []byte("")})
	wshandler.EmitMessage(jsonstr)
	json, err := json.Marshal(testEvent.Item)
	json = append(json, ',')

	time.Sleep(1 * time.Second)

	cached, err := mc.Get(key)
	if assert.NoError(t, err) {
		assert.Equal(t, string(json), string(cached.Value))
	}

	// test deleteExcessiveSubs()
	m.Subscribe(&conn1, []string{"local0", "local1"})
	assert.Len(t, m.remoteSubs[domain], 2) // 普通に増える
	m.deleteExcessiveSubs()
	assert.Len(t, m.remoteSubs[domain], 0) // リセットされる

	// test updateChunks()
	/*
	key0 := "stream:body:all:" + remotestream0 + ":" + stream.Time2Chunk(pivot)
	mc.Set(&memcache.Item{
		Key: key0,
		Value: []byte(""),
	})
	key1 := "stream:body:all:" + remotestream1 + ":" + stream.Time2Chunk(pivot)
	mc.Set(&memcache.Item{
		Key: key1,
		Value: []byte(""),
	})

	// remotestream1だけ残しておく
	m.Subscribe(&conn1, []string{remotestream1})

	// 新しいpivotで更新
	newPivot := pivot.Add(10 * time.Minute)
	m.updateChunks(stream.Time2Chunk(newPivot))

	// remotestream1だけ残っていることを確認

	newkey0 := "stream:body:all:" + remotestream0 + ":" + stream.Time2Chunk(newPivot)
	_, err = mc.Get(newkey0)
	assert.Error(t, err)

	newkey1 := "stream:body:all:" + remotestream1 + ":" + stream.Time2Chunk(newPivot)
	newdat1, err := mc.Get(newkey1)
	if assert.NoError(t, err) {
		assert.Equal(t, newdat1.Value, []byte(""))
	}
	*/
}

