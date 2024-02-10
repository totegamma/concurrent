package testutil

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ory/dockertest"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/x/core"
)

var (
	user        = "postgres"
	password    = "secret"
	dbName      = "unittest"
	dialect     = "postgres"
	dsnTemplate = "postgres://%s:%s@localhost:%s/%s?sslmode=disable"
)

var pool *dockertest.Pool
var poolLock = &sync.Mutex{}
var dbLock = &sync.Mutex{}

func CreateDB() (*gorm.DB, func()) {
	dbLock.Lock()
	defer dbLock.Unlock()

	pool := getPool()

	runOptions := &dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "latest",
		Env: []string{
			"POSTGRES_USER=" + user,
			"POSTGRES_PASSWORD=" + password,
			"POSTGRES_DB=" + dbName,
		},
		ExposedPorts: []string{"5432/tcp"},
	}

	resource, err := pool.RunWithOptions(runOptions)
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}
	cleanup := func() {
		closeContainer(pool, resource)
	}

	port := resource.GetPort("5432/tcp")
	log.Printf("Postgres running on port %s\n", port)
	dsn := fmt.Sprintf(dsnTemplate, user, password, port, dbName)
	log.Printf("DSN: %s\n", dsn)

	var db *gorm.DB
	if err := pool.Retry(func() error {
		time.Sleep(time.Second * 2)

		var err error

		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		return err
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}

	db.AutoMigrate(
		&core.Message{},
		&core.Character{},
		&core.Association{},
		&core.Stream{},
		&core.StreamItem{},
		&core.Domain{},
		&core.Entity{},
		&core.EntityMeta{},
		&core.Address{},
		&core.Collection{},
		&core.CollectionItem{},
		&core.Ack{},
		&core.Key{},
	)

	return db, cleanup
}

func CreateMC() (*memcache.Client, func()) {

	pool := getPool()

	runOptions := &dockertest.RunOptions{
		Repository: "memcached",
		Tag:        "1.6.7",
		Env: []string{
			"MEMCACHED_ENABLE_TLS=false",
		},
		ExposedPorts: []string{"11211/tcp"},
	}

	resource, err := pool.RunWithOptions(runOptions)
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}
	cleanup := func() {
		closeContainer(pool, resource)
	}

	port := resource.GetPort("11211/tcp")
	log.Printf("Memcached running on port %s", port)

	// Memcached(コンテナ)との接続
	var client *memcache.Client
	if err := pool.Retry(func() error {
		time.Sleep(time.Second * 1)

		var err error
		client = memcache.New("localhost:" + port)
		return err
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}
	return client, cleanup
}

func CreateRDB() (*redis.Client, func()) {

	pool := getPool()

	runOptions := &dockertest.RunOptions{
		Repository: "redis",
		Tag:        "latest",
		Env: []string{
			"REDIS_PASSWORD=secret",
		},
		ExposedPorts: []string{"6379/tcp"},
	}

	resource, err := pool.RunWithOptions(runOptions)
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}
	cleanup := func() {
		closeContainer(pool, resource)
	}

	port := resource.GetPort("6379/tcp")
	log.Printf("Redis running on port %s", port)

	// Redis(コンテナ)との接続
	var client *redis.Client
	if err := pool.Retry(func() error {
		time.Sleep(time.Second * 1)

		var err error
		client = redis.NewClient(&redis.Options{
			Addr:     "localhost:" + port,
			Password: "secret",
			DB:       0,
		})
		return err
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}
	return client, cleanup
}

func closeContainer(pool *dockertest.Pool, resource *dockertest.Resource) {
	// コンテナの終了
	if err := pool.Purge(resource); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
}

func getPool() *dockertest.Pool {
	poolLock.Lock()
	defer poolLock.Unlock()
	if pool == nil {
		var err error
		pool, err = dockertest.NewPool("")
		pool.MaxWait = time.Second * 10
		if err != nil {
			log.Fatalf("Could not connect to docker: %s", err)
		}
	}
	return pool
}
