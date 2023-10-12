package testutil


import (
	"fmt"
	"log"
	"time"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/x/core"
)

var (
	user     = "postgres"
	password = "secret"
	dbName   = "unittest"
	port     = "5433"
	dialect  = "postgres"
	dsn      = "postgres://%s:%s@localhost:%s/%s?sslmode=disable"
)


func CreateDBContainer() (*dockertest.Resource, *dockertest.Pool) {

	pool, err := dockertest.NewPool("")
	pool.MaxWait = time.Minute * 2
	if err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}

	runOptions := &dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "latest",
		Env: []string{
			"POSTGRES_USER=" + user,
			"POSTGRES_PASSWORD=" + password,
			"POSTGRES_DB=" + dbName,
		},
		ExposedPorts: []string{"5432"},
	    PortBindings: map[docker.Port][]docker.PortBinding{
		  "5432": {
			 {HostIP: "0.0.0.0", HostPort: port},
		  },
	    },
	}

	resource, err := pool.RunWithOptions(runOptions)
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	return resource, pool
}

func CreateMemcachedContainer() (*dockertest.Resource, *dockertest.Pool) {
	
	pool, err := dockertest.NewPool("")
	pool.MaxWait = time.Minute * 2
	if err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}

	runOptions := &dockertest.RunOptions{
		Repository: "memcached",
		Tag:        "1.6.7",
		Env: []string{
			"MEMCACHED_ENABLE_TLS=false",
		},
		ExposedPorts: []string{"11211"},
	    PortBindings: map[docker.Port][]docker.PortBinding{
			"11211": {
				{HostIP: "0.0.0.0", HostPort: "11211"},
			},
	 	},
	}

	resource, err := pool.RunWithOptions(runOptions)
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	return resource, pool
}

func CloseContainer(resource *dockertest.Resource, pool *dockertest.Pool) {
	// コンテナの終了
	log.Println("Bye")
	if err := pool.Purge(resource); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
}

func ConnectDB(resource *dockertest.Resource, pool *dockertest.Pool) *gorm.DB {
	// DB(コンテナ)との接続
	var db *gorm.DB
	if err := pool.Retry(func() error {
		time.Sleep(time.Second * 10)

		var err error
		dsn = fmt.Sprintf(dsn, user, password, port, dbName)

		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		return err
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}
	return db
}

func SetupDB(conn *gorm.DB) {
	log.Println("start migrate")
	conn.AutoMigrate(
		&core.Message{},
		&core.Character{},
		&core.Association{},
		&core.Stream{},
		&core.StreamItem{},
		&core.Domain{},
		&core.Entity{},
		&core.Collection{},
		&core.CollectionItem{},
		&core.Ack{},
	)
}

func ConnectMemcached(resource *dockertest.Resource, pool *dockertest.Pool) *memcache.Client {
	// Memcached(コンテナ)との接続
	var client *memcache.Client
	if err := pool.Retry(func() error {
		time.Sleep(time.Second * 10)

		var err error
		client = memcache.New("localhost:11211")
		return err
	}); err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}
	return client
}

/*
func TestMain(m *testing.M) {
	log.Println("Test Start")
	db_resource, db_pool := createDBContainer()
	defer closeContainer(db_resource, db_pool)

	db = connectDB(db_resource, db_pool)

	setupDB(db)

	mc_resource, mc_pool := createMemcachedContainer()
	defer closeContainer(mc_resource, mc_pool)

	mc = connectMemcached(mc_resource, mc_pool)

	m.Run()

	log.Println("Test End")
}
*/
