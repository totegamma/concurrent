package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/character"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/stream"
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
	"go.opentelemetry.io/otel/trace"
	"gorm.io/plugin/opentelemetry/tracing"
)

type CustomHandler struct {
	slog.Handler
}

func (h *CustomHandler) Handle(ctx context.Context, r slog.Record) error {

	r.AddAttrs(slog.String("type", "app"))

	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		r.AddAttrs(slog.String("traceID", span.SpanContext().TraceID().String()))
		r.AddAttrs(slog.String("spanID", span.SpanContext().SpanID().String()))
	}

	return h.Handler.Handle(ctx, r)
}

var (
	version      = "unknown"
	buildMachine = "unknown"
	buildTime    = "unknown"
	goVersion    = "unknown"
)

func main() {

	fmt.Fprint(os.Stderr, concurrentBanner)

	handler := &CustomHandler{Handler: slog.NewJSONHandler(os.Stdout, nil)}
	slogger := slog.New(handler)
	slog.SetDefault(slogger)

	slog.Info(fmt.Sprintf("Concurrent %s starting...", version))

	e := echo.New()
	e.HidePort = true
	e.HideBanner = true
	config := util.Config{}
	configPath := os.Getenv("CONCURRENT_CONFIG")
	if configPath == "" {
		configPath = "/etc/concurrent/config.yaml"
	}

	err := config.Load(configPath)
	if err != nil {
		slog.Error("Failed to load config: ", err)
	}

	slog.Info(fmt.Sprintf("Config loaded! I am: %s", config.Concurrent.CCID))

	if config.Server.EnableTrace {
		cleanup, err := setupTraceProvider(config.Server.TraceEndpoint, config.Concurrent.FQDN+"/ccapi", version)
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

	e.Use(echoprometheus.NewMiddlewareWithConfig(echoprometheus.MiddlewareConfig{
		Namespace: "ccapi",
		LabelFuncs: map[string]echoprometheus.LabelValueFunc{
			"url": func(c echo.Context, err error) string {
				return "REDACTED"
			},
		},
		Skipper: func(c echo.Context) bool {
			return c.Path() == "/metrics" || c.Path() == "/health"
		},
	}))

	e.Use(middleware.Recover())

	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             300 * time.Millisecond, // Slow SQL threshold
			LogLevel:                  logger.Warn,            // Log level
			IgnoreRecordNotFoundError: true,                   // Ignore ErrRecordNotFound error for logger
			Colorful:                  true,                   // Enable color
		},
	)

	db, err := gorm.Open(postgres.Open(config.Server.Dsn), &gorm.Config{
		Logger: gormLogger,
	})
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
	slog.Info("start migrate")
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
	if err != nil {
		panic("failed to connect memcached")
	}
	defer mc.Close()

	agent := SetupAgent(db, rdb, mc, config)

	socketManager := SetupSocketManager(mc, db, rdb, config)
	socketHandler := SetupSocketHandler(rdb, socketManager, config)
	domainHandler := SetupDomainHandler(db, config)
	userkvHandler := SetupUserkvHandler(db, rdb, mc, config)
	collectionHandler := SetupCollectionHandler(db, rdb, config)

	messageService := SetupMessageService(db, rdb, mc, socketManager, config)
	messageHandler := message.NewHandler(messageService)

	associationService := SetupAssociationService(db, rdb, mc, socketManager, config)
	associationHandler := association.NewHandler(associationService)

	characterService := SetupCharacterService(db, mc, config)
	characterHandler := character.NewHandler(characterService)

	streamService := SetupStreamService(db, rdb, mc, socketManager, config)
	streamHandler := stream.NewHandler(streamService)

	entityService := SetupEntityService(db, rdb, mc, config)
	entityHandler := entity.NewHandler(entityService, config)

	authService := SetupAuthService(db, rdb, mc, config)
	authHandler := auth.NewHandler(authService)

	apiV1 := e.Group("", auth.ParseJWT)
	// domain
	apiV1.GET("/domain", domainHandler.Profile)
	apiV1.GET("/domain/:id", domainHandler.Get)
	apiV1.POST("/domain/:id", domainHandler.SayHello, authService.Restrict(auth.ISADMIN))
	apiV1.PUT("/domain/:id", domainHandler.Upsert, authService.Restrict(auth.ISADMIN))
	apiV1.DELETE("/domain/:id", domainHandler.Delete, authService.Restrict(auth.ISADMIN))
	apiV1.GET("/domains", domainHandler.List)

	apiV1.POST("/domains/hello", domainHandler.Hello)

	// address
	apiV1.GET("/address/:id", entityHandler.Resolve)

	// entity
	apiV1.POST("/entity", entityHandler.Register)
	apiV1.GET("/entity/:id", entityHandler.Get)
	apiV1.PUT("/entity/:id", entityHandler.Update, authService.Restrict(auth.ISADMIN))
	apiV1.DELETE("/entity/:id", entityHandler.Delete, authService.Restrict(auth.ISADMIN))
	apiV1.GET("/entity/:id/acking", entityHandler.GetAcking)
	apiV1.GET("/entity/:id/acker", entityHandler.GetAcker)
	apiV1.GET("/entities", entityHandler.List)
	apiV1.POST("/entities/ack", entityHandler.Ack, authService.Restrict(auth.ISLOCAL))
	apiV1.POST("/entities/checkpoint/ack", entityHandler.Ack, authService.Restrict(auth.ISUNITED))

	apiV1.PUT("/tmp/entity/:id", entityHandler.UpdateRegistration, authService.Restrict(auth.ISLOCAL)) // NOTE: for migration. Remove later

	apiV1.POST("/admin/entity", entityHandler.Create, authService.Restrict(auth.ISADMIN))

	// message
	apiV1.POST("/message", messageHandler.Post, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/message/:id", messageHandler.Get)
	apiV1.DELETE("/message/:id", messageHandler.Delete, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/message/:id/associations", associationHandler.GetFiltered)
	apiV1.GET("/message/:id/associationcounts", associationHandler.GetCounts)
	apiV1.GET("/message/:id/associations/mine", associationHandler.GetOwnByTarget, authService.Restrict(auth.ISKNOWN))

	// association
	apiV1.POST("/association", associationHandler.Post, authService.Restrict(auth.ISKNOWN))
	apiV1.GET("/association/:id", associationHandler.Get)
	apiV1.DELETE("/association/:id", associationHandler.Delete, authService.Restrict(auth.ISKNOWN))

	// character
	apiV1.PUT("/character", characterHandler.Put, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/characters", characterHandler.Get)

	// stream
	apiV1.POST("/stream", streamHandler.Create, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/stream/:id", streamHandler.Get)
	apiV1.PUT("/stream/:id", streamHandler.Update, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/stream/:id", streamHandler.Delete, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/stream/:stream/:object", streamHandler.Remove, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/streams", streamHandler.List)
	apiV1.GET("/streams/mine", streamHandler.ListMine)
	apiV1.GET("/streams/recent", streamHandler.Recent)
	apiV1.GET("/streams/range", streamHandler.Range)
	apiV1.GET("/streams/chunks", streamHandler.GetChunks)
	apiV1.POST("/streams/checkpoint", streamHandler.Checkpoint, authService.Restrict(auth.ISUNITED))      // OLD API Remove for next release
	apiV1.POST("/streams/checkpoint/item", streamHandler.Checkpoint, authService.Restrict(auth.ISUNITED)) // NEW API will be used for next release
	apiV1.POST("/streams/checkpoint/event", streamHandler.EventCheckpoint, authService.Restrict(auth.ISUNITED))

	// userkv
	apiV1.GET("/kv/:key", userkvHandler.Get, authService.Restrict(auth.ISLOCAL))
	apiV1.PUT("/kv/:key", userkvHandler.Upsert, authService.Restrict(auth.ISLOCAL))

	// socket
	apiV1.GET("/socket", socketHandler.Connect)

	// auth
	apiV1.GET("/auth/passport/:remote", authHandler.GetPassport)

	// collection
	apiV1.POST("/collection", collectionHandler.CreateCollection, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/collection/:id", collectionHandler.GetCollection)
	apiV1.PUT("/collection/:id", collectionHandler.UpdateCollection, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/collection/:id", collectionHandler.DeleteCollection, authService.Restrict(auth.ISLOCAL))

	apiV1.POST("/collection/:collection", collectionHandler.CreateItem, authService.Restrict(auth.ISLOCAL))
	apiV1.GET("/collection/:collection/:item", collectionHandler.GetItem)
	apiV1.PUT("/collection/:collection/:item", collectionHandler.UpdateItem, authService.Restrict(auth.ISLOCAL))
	apiV1.DELETE("/collection/:collection/:item", collectionHandler.DeleteItem, authService.Restrict(auth.ISLOCAL))

	// misc
	apiV1.GET("/profile", func(c echo.Context) error {
		profile := config.Profile
		profile.Registration = config.Concurrent.Registration
		profile.Version = version
		profile.BuildInfo = util.BuildInfo{
			BuildTime:    buildTime,
			BuildMachine: buildMachine,
			GoVersion:    goVersion,
		}
		profile.SiteKey = config.Server.CaptchaSitekey
		return c.JSON(http.StatusOK, profile)
	})
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

	var streamSubscriptionMetrics = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cc_stream_subscriptions",
			Help: "stream subscriptions",
		},
		[]string{"stream"},
	)
	prometheus.MustRegister(streamSubscriptionMetrics)

	var resourceCountMetrics = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cc_resources_count",
			Help: "resources count",
		},
		[]string{"type"},
	)
	prometheus.MustRegister(resourceCountMetrics)

	var socketConnectionMetrics = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "cc_socket_connections",
			Help: "socket connections",
		},
	)
	prometheus.MustRegister(socketConnectionMetrics)

	go func() {
		for {
			time.Sleep(15 * time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			subscriptions, err := streamService.ListStreamSubscriptions(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to list stream subscriptions: %v", err))
				continue
			}
			for stream, count := range subscriptions {
				streamSubscriptionMetrics.WithLabelValues(stream).Set(float64(count))
			}

			count, err := messageService.Count(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to count messages: %v", err))
				continue
			}
			resourceCountMetrics.WithLabelValues("message").Set(float64(count))

			count, err = entityService.Count(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to count entities: %v", err))
				continue
			}
			resourceCountMetrics.WithLabelValues("entity").Set(float64(count))

			count, err = characterService.Count(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to count characters: %v", err))
				continue
			}
			resourceCountMetrics.WithLabelValues("character").Set(float64(count))

			count, err = associationService.Count(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to count associations: %v", err))
				continue
			}
			resourceCountMetrics.WithLabelValues("association").Set(float64(count))

			count, err = streamService.Count(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to count streams: %v", err))
				continue
			}
			resourceCountMetrics.WithLabelValues("stream").Set(float64(count))

			count = socketHandler.CurrentConnectionCount()
			socketConnectionMetrics.Set(float64(count))
		}
	}()

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
			slog.Error(fmt.Sprintf("Failed to shutdown tracer provider: %v", err))
		}
	}
	return cleanup, nil
}
