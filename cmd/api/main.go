//go:generate go run github.com/google/wire/cmd/wire gen .
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"gorm.io/plugin/opentelemetry/tracing"
)

func main() {

	fmt.Print(concurrentBanner)

	e := echo.New()
	config := util.Config{}
	configPath := os.Getenv("CONCURRENT_CONFIG")
	if configPath == "" {
		configPath = "/etc/concurrent/config.yaml"
	}

	err := config.Load(configPath)
	if err != nil {
		e.Logger.Fatal(err)
	}

	log.Print("Concurrent ", util.GetFullVersion(), " starting...")
	log.Print("Config loaded! I am: ", config.Concurrent.CCID)

	logfile, err := os.OpenFile(filepath.Join(config.Server.LogPath, "api-access.log"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer logfile.Close()

	// e.Logger.SetOutput(logfile)

	e.HidePort = true
	e.HideBanner = true

	if config.Server.EnableTrace {
		cleanup, err := setupTraceProvider(config.Server.TraceEndpoint, config.Concurrent.FQDN+"/ccapi", util.GetFullVersion())
		if err != nil {
			panic(err)
		}
		defer cleanup()

		skipper := otelecho.WithSkipper(
			func(c echo.Context) bool {
				return c.Path() == "/metrics" || c.Path() == "/health"
			},
		)
		e.Use(otelecho.Middleware("api", skipper))
	}

	e.Use(echoprometheus.NewMiddleware("ccapi"))
	//e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	db, err := gorm.Open(postgres.Open(config.Server.Dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	sqlDB, err := db.DB() // for pinging
	if err != nil {
		panic("failed to connect database")
	}
	defer sqlDB.Close()

	err = db.Use(tracing.NewPlugin(
		tracing.WithDBName("postgres"),
	))
	if err != nil {
		panic("failed to setup tracing plugin")
	}

	// Migrate the schema
	log.Println("start migrate")
	db.AutoMigrate(
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

	rdb := redis.NewClient(&redis.Options{
		Addr:     config.Server.RedisAddr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	err = redisotel.InstrumentTracing(
		rdb,
		redisotel.WithAttributes(
			attribute.KeyValue{
				Key:   "db.name",
				Value: attribute.StringValue("redis"),
			},
		),
	)
	if err != nil {
		panic("failed to setup tracing plugin")
	}

	mc := memcache.New(config.Server.MemcachedAddr)
	log.Println("config.Server.MemcachedAddr", config.Server.MemcachedAddr)
	if err != nil {
		panic("failed to connect memcached")
	}
	defer mc.Close()

	agent := SetupAgent(db, rdb, config)

	socketManager := SetupSocketManager(mc, db, rdb, config)
	socketHandler := SetupSocketHandler(rdb, config, socketManager)
	messageHandler := SetupMessageHandler(db, rdb, mc, config)
	characterHandler := SetupCharacterHandler(db, config)
	associationHandler := SetupAssociationHandler(db, rdb, mc, config)
	streamHandler := SetupStreamHandler(db, rdb, mc, config)
	domainHandler := SetupDomainHandler(db, config)
	entityHandler := SetupEntityHandler(db, rdb, config)
	authHandler := SetupAuthHandler(db, config)
	userkvHandler := SetupUserkvHandler(db, rdb, config)
	collectionHandler := SetupCollectionHandler(db, rdb, config)

	authService := SetupAuthService(db, config)

	apiV1 := e.Group("", auth.ParseJWT)
	apiV1.GET("/message/:id", messageHandler.Get)
	apiV1.GET("/message/:id/associations", associationHandler.GetFiltered)
	apiV1.GET("/message/:id/associationcounts", associationHandler.GetCounts)
	apiV1.GET("/message/:id/associations/own", associationHandler.GetOwnByTarget, authService.Restrict(auth.ISKNOWN))
	apiV1.GET("/characters", characterHandler.Get)
	apiV1.GET("/association/:id", associationHandler.Get)
	apiV1.GET("/stream/:id", streamHandler.Get)
	apiV1.GET("/streams", streamHandler.List)
	apiV1.GET("/streams/recent", streamHandler.Recent)
	apiV1.GET("/streams/range", streamHandler.Range)
	apiV1.GET("/socket", socketHandler.Connect)
	apiV1.GET("/domain", domainHandler.Profile)
	apiV1.GET("/domain/:id", domainHandler.Get)
	apiV1.GET("/domains", domainHandler.List)
	apiV1.POST("/domains/hello", domainHandler.Hello)
	apiV1.GET("/entity/:id", entityHandler.Get)
	apiV1.GET("/entity/:id/acking", entityHandler.GetAcking)
	apiV1.GET("/entity/:id/acker", entityHandler.GetAcker)
	apiV1.GET("/entities", entityHandler.List)
	apiV1.GET("/auth/claim", authHandler.Claim)
	apiV1.GET("/profile", func(c echo.Context) error {
		profile := config.Profile
		profile.Registration = config.Concurrent.Registration
		profile.Version = util.GetVersion()
		profile.Hash = util.GetGitHash()
		profile.SiteKey = config.Server.CaptchaSitekey
		return c.JSON(http.StatusOK, profile)
	})

	apiV1.PUT("/domain", domainHandler.Upsert, authService.Restrict(auth.ISADMIN))
	apiV1.DELETE("/domain/:id", domainHandler.Delete, authService.Restrict(auth.ISADMIN))
	apiV1.GET("/admin/sayhello/:fqdn", domainHandler.SayHello, authService.Restrict(auth.ISADMIN))

	apiV1.POST("/entity", entityHandler.Register, authService.Restrict(auth.ISUNKNOWN))
	apiV1.DELETE("/entity/:id", entityHandler.Delete, authService.Restrict(auth.ISADMIN))
	apiV1.PUT("/entity/:id", entityHandler.Update, authService.Restrict(auth.ISADMIN))
	apiV1.POST("/entities/ack", entityHandler.Ack, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/entities/ack", entityHandler.Unack, authService.Restrict(auth.ISLOCAL))
	apiV1.POST("/entities/checkpoint/ack", entityHandler.Ack, authService.Restrict(auth.ISUNITED))
	apiV1.DELETE("/entities/checkpoint/ack", entityHandler.Unack, authService.Restrict(auth.ISUNITED))

	apiV1.POST("/admin/entity", entityHandler.Create, authService.Restrict(auth.ISADMIN))

	apiV1.POST("/message", messageHandler.Post, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/message/:id", messageHandler.Delete, authService.Restrict(auth.ISLOCAL))

	apiV1.PUT("/character", characterHandler.Put, authService.Restrict(auth.ISLOCAL))

	apiV1.POST("/association", associationHandler.Post, authService.Restrict(auth.ISKNOWN))
	apiV1.DELETE("/association/:id", associationHandler.Delete, authService.Restrict(auth.ISKNOWN))

	apiV1.POST("/stream", streamHandler.Create, authService.Restrict(auth.ISLOCAL))
	apiV1.PUT("/stream/:id", streamHandler.Update, authService.Restrict(auth.ISLOCAL))
	apiV1.POST("/streams/checkpoint", streamHandler.Checkpoint, authService.Restrict(auth.ISUNITED))
	apiV1.DELETE("/stream/:id", streamHandler.Delete, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/stream/:stream/:object", streamHandler.Remove, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/streams/mine", streamHandler.ListMine)
	apiV1.GET("/streams/chunks", streamHandler.GetChunks)

	apiV1.GET("/kv/:key", userkvHandler.Get, authService.Restrict(auth.ISLOCAL))
	apiV1.PUT("/kv/:key", userkvHandler.Upsert, authService.Restrict(auth.ISLOCAL))

	apiV1.POST("/collection", collectionHandler.CreateCollection, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/collection/:id", collectionHandler.GetCollection)
	apiV1.PUT("/collection/:id", collectionHandler.UpdateCollection, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/collection/:id", collectionHandler.DeleteCollection, authService.Restrict(auth.ISLOCAL))

	apiV1.POST("/collection/:collection", collectionHandler.CreateItem, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/collection/:collection/:item", collectionHandler.GetItem)
	apiV1.PUT("/collection/:collection/:item", collectionHandler.UpdateItem, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/collection/:collection/:item", collectionHandler.DeleteItem, authService.Restrict(auth.ISLOCAL))

	e.GET("/health", func(c echo.Context) (err error) {
		ctx := c.Request().Context()

		err = sqlDB.Ping()
		if err != nil {
			return c.String(http.StatusInternalServerError, "db error")
		}

		err = rdb.Ping(ctx).Err()
		if err != nil {
			return c.String(http.StatusInternalServerError, "redis error")
		}

		return c.String(http.StatusOK, "ok")
	})

	e.GET("/metrics", echoprometheus.NewHandler())

	agent.Boot()

	e.Logger.Fatal(e.Start(":8000"))
}

func setupTraceProvider(endpoint string, serviceName string, serviceVersion string) (func(), error) {

	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)

	if err != nil {
		return nil, err
	}

	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceVersionKey.String(serviceVersion),
	)

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resource),
	)
	otel.SetTracerProvider(tracerProvider)

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	cleanup := func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := tracerProvider.Shutdown(ctx); err != nil {
			log.Printf("Failed to shutdown tracer provider: %v", err)
		}
	}
	return cleanup, nil
}
