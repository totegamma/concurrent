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
	log.Print("Config loaded! I am: ", config.Concurrent.CCAddr)

	logfile, err := os.OpenFile(filepath.Join(config.Server.LogPath, "api-access.log"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer logfile.Close()

	e.Logger.SetOutput(logfile)

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
		e.Use(otelecho.Middleware("dev", skipper))

		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				span := trace.SpanFromContext(c.Request().Context())
				c.Response().Header().Set("trace-id", span.SpanContext().TraceID().String())
				return next(c)
			}
		})
	}

	e.Use(echoprometheus.NewMiddleware("ccapi"))
	e.Use(middleware.Logger())
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
		&core.Host{},
		&core.Entity{},
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

	agent := SetupAgent(db, rdb, config)

	socketHandler := SetupSocketHandler(rdb, config)
	messageHandler := SetupMessageHandler(db, rdb, config)
	characterHandler := SetupCharacterHandler(db, config)
	associationHandler := SetupAssociationHandler(db, rdb, config)
	streamHandler := SetupStreamHandler(db, rdb, config)
	hostHandler := SetupHostHandler(db, config)
	entityHandler := SetupEntityHandler(db, config)
	authHandler := SetupAuthHandler(db, config)
	userkvHandler := SetupUserkvHandler(db, rdb, config)

	authService := SetupAuthService(db, config)

	apiV1 := e.Group("")
	apiV1.GET("/messages/:id", messageHandler.Get)
	apiV1.GET("/characters", characterHandler.Get)
	apiV1.GET("/associations/:id", associationHandler.Get)
	apiV1.GET("/stream", streamHandler.Get)
	apiV1.GET("/stream/recent", streamHandler.Recent)
	apiV1.GET("/stream/list", streamHandler.List)
	apiV1.GET("/stream/range", streamHandler.Range)
	apiV1.GET("/socket", socketHandler.Connect)
	apiV1.GET("/host/:id", hostHandler.Get) //TODO deprecated. remove later
	apiV1.GET("/host", hostHandler.Profile)
	apiV1.GET("/host/list", hostHandler.List)
	apiV1.GET("/entity/:id", entityHandler.Get)
	apiV1.GET("/entity/list", entityHandler.List)
	apiV1.GET("/auth/claim", authHandler.Claim)
	apiV1.GET("/profile", func(c echo.Context) error {
		profile := config.Profile
		profile.Registration = config.Concurrent.Registration
		profile.Version = util.GetVersion()
		profile.Hash = util.GetGitHash()
		return c.JSON(http.StatusOK, profile)
	})

	apiV1R := apiV1.Group("", auth.JWT)
	apiV1R.PUT("/host", hostHandler.Upsert, authService.Restrict(auth.ISADMIN))
	apiV1R.POST("/host/hello", hostHandler.Hello, authService.Restrict(auth.ISUNUNITED))
	apiV1R.DELETE("/host/:id", hostHandler.Delete, authService.Restrict(auth.ISADMIN))
	apiV1R.GET("/admin/sayhello/:fqdn", hostHandler.SayHello, authService.Restrict(auth.ISADMIN))

	apiV1R.POST("/entity", entityHandler.Register, authService.Restrict(auth.ISUNKNOWN))
	apiV1R.PUT("/entity", entityHandler.Update, authService.Restrict(auth.ISLOCAL))
	apiV1R.DELETE("/entity/:id", entityHandler.Delete, authService.Restrict(auth.ISADMIN))
	apiV1R.POST("/admin/entity", entityHandler.Register, authService.Restrict(auth.ISADMIN))

	apiV1R.POST("/messages", messageHandler.Post, authService.Restrict(auth.ISLOCAL))
	apiV1R.DELETE("/messages", messageHandler.Delete, authService.Restrict(auth.ISLOCAL))

	apiV1R.PUT("/characters", characterHandler.Put, authService.Restrict(auth.ISLOCAL))

	apiV1R.POST("/associations", associationHandler.Post, authService.Restrict(auth.ISKNOWN))
	apiV1R.DELETE("/associations", associationHandler.Delete, authService.Restrict(auth.ISKNOWN))

	apiV1R.PUT("/stream", streamHandler.Put, authService.Restrict(auth.ISLOCAL))
	apiV1R.POST("/stream/checkpoint", streamHandler.Checkpoint, authService.Restrict(auth.ISUNITED))

	apiV1R.GET("/kv/:key", userkvHandler.Get, authService.Restrict(auth.ISLOCAL))
	apiV1R.PUT("/kv/:key", userkvHandler.Upsert, authService.Restrict(auth.ISLOCAL))


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
