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

func TestManager(t *testing.T) {
	conn0 := websocket.Conn{}
	m.Subscribe(&conn0, []string{"test0", "test1"})
	assert.Len(t, m.clientSubs, 1)
	assert.Len(t, m.clientSubs[&conn0], 2)
	assert.Len(t, m.remoteSubs, 0)
}

