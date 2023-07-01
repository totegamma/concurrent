package main

import (
	"bytes"
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

	"github.com/totegamma/concurrent/x/activitypub"
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

    logfile, err := os.OpenFile(filepath.Join(config.Server.LogPath, "access.log"), os.O_RDWR | os.O_CREATE | os.O_APPEND, 0666)
    if err != nil {
        log.Fatal(err)
    }
    defer logfile.Close()

    e.HidePort = true
    e.HideBanner = true
    e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
        AllowOrigins: []string{"*"},
        AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
        ExposeHeaders: []string{"trace-id"},
    }))

    if config.Server.EnableTrace {
        cleanup, err := setupTraceProvider(config.Server.TraceEndpoint, config.Concurrent.FQDN + "/concurrent", util.GetFullVersion())
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

    e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
        Skipper: func(c echo.Context) bool {
            return c.Path() == "/metrics" || c.Path() == "/health"
        },
        Format: `{"time":"${time_rfc3339_nano}",${custom},"remote_ip":"${remote_ip}",` +
                `"host":"${host}","method":"${method}","uri":"${uri}","status":${status},` +
                `"error":"${error}","latency":${latency},"latency_human":"${latency_human}",` +
                `"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n",
        CustomTagFunc: func(c echo.Context, buf *bytes.Buffer) (int, error) {
            span := trace.SpanFromContext(c.Request().Context())
            buf.WriteString(fmt.Sprintf("\"%s\":\"%s\"", "traceID", span.SpanContext().TraceID().String()))
            buf.WriteString(fmt.Sprintf(",\"%s\":\"%s\"", "spanID", span.SpanContext().SpanID().String()))
            return 0, nil
        },
    }))

    e.Use(echoprometheus.NewMiddleware("concurrent"))
    e.Use(middleware.Recover())

    e.Logger.SetOutput(logfile)
    e.Binder = &activitypub.Binder{}


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
        &activitypub.ApEntity{},
        &activitypub.ApPerson{},
        &activitypub.ApFollow{},
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
                Key: "db.name",
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
    activitypubHandler := SetupActivitypubHandler(db, rdb, config)

    e.GET("/.well-known/webfinger", activitypubHandler.WebFinger)
    e.GET("/.well-known/nodeinfo", activitypubHandler.NodeInfoWellKnown)
    e.GET("/ap/nodeinfo/2.0", activitypubHandler.NodeInfo)

    ap := e.Group("/ap")
    ap.GET("/acct/:id", activitypubHandler.User)
    ap.POST("/acct/:id/inbox", activitypubHandler.Inbox)
    ap.POST("/acct/:id/outbox", activitypubHandler.PrintRequest)
    ap.GET("/note/:id", activitypubHandler.Note)

    apiV1 := e.Group("/api/v1")
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
    apiV1.GET("/ap/entity/:ccaddr", activitypubHandler.GetEntityID)
    apiV1.GET("/ap/person/:id", activitypubHandler.GetPerson)
    apiV1.GET("/profile", func (c echo.Context) error {
        profile := config.Profile
        profile.Version = util.GetVersion()
        profile.Hash = util.GetGitHash()
        return c.JSON(http.StatusOK, profile)
    })

    apiV1R := apiV1.Group("", auth.JWT)
    apiV1R.POST("/messages", messageHandler.Post)
    apiV1R.DELETE("/messages", messageHandler.Delete)
    apiV1R.PUT("/characters", characterHandler.Put)
    apiV1R.POST("/associations", associationHandler.Post)
    apiV1R.DELETE("/associations", associationHandler.Delete)
    apiV1R.PUT("/stream", streamHandler.Put)
    apiV1R.POST("/stream/checkpoint", streamHandler.Checkpoint)
    apiV1R.PUT("/host", hostHandler.Upsert)
    apiV1R.POST("/entity", entityHandler.Post)
    apiV1R.GET("/admin/sayhello/:fqdn", hostHandler.SayHello)
    apiV1R.GET("/kv/:key", userkvHandler.Get)
    apiV1R.PUT("/kv/:key", userkvHandler.Upsert)
    apiV1R.POST("/ap/entity", activitypubHandler.CreateEntity)
    apiV1R.PUT("/ap/person", activitypubHandler.UpdatePerson)
    apiV1R.POST("/host/hello", hostHandler.Hello)

    apiV1R.DELETE("/host/:id", hostHandler.Delete)
    apiV1R.DELETE("/entity/:id", entityHandler.Delete)



    e.GET("/*", spa)
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
    go activitypubHandler.Boot()

    e.Logger.Fatal(e.Start(":8000"))
}

func spa(c echo.Context) error {
    path := c.Request().URL.Path

    webFilePath := os.Getenv("CONCURRENT_WEBUI")
    if webFilePath == "" {
        webFilePath = "/etc/www/concurrent"
    }
    fPath := filepath.Join(webFilePath, path)
    if _, err := os.Stat(fPath); os.IsNotExist(err) {
        return c.File(filepath.Join(webFilePath,"index.html"))
    }
    return c.File(fPath)
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

