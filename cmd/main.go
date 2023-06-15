package main

import (
    "os"
    "fmt"
    "log"
    "bytes"
    "context"
    "net/http"
    "runtime/debug"
    "path/filepath"

    "github.com/redis/go-redis/v9"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"

    "github.com/totegamma/concurrent/x/auth"
    "github.com/totegamma/concurrent/x/core"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/activitypub"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
    "go.opentelemetry.io/otel/trace"
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

    buildinfo, ok := debug.ReadBuildInfo()
    var version string = "unknown"
    if ok {
        version = buildinfo.Main.Version
    }

    log.Print("Concurrent ", version, " starting...")
    log.Print("Config loaded! I am: ", config.Concurrent.CCAddr)

    // Setup tracing
    cleanup, err := SetupTraceProvider(config.Server.TraceEndpoint, "api", version)
    if err != nil {
        panic(err)
    }
    defer cleanup()

    db, err := gorm.Open(postgres.Open(config.Server.Dsn), &gorm.Config{})
    if err != nil {
        log.Println("failed to connect database");
        panic("failed to connect database")
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

    logfile, err := os.OpenFile(filepath.Join(config.Server.LogPath, "access.log"), os.O_RDWR | os.O_CREATE | os.O_APPEND, 0666)
    if err != nil {
        log.Fatal(err)
    }
    defer logfile.Close()

    e.HidePort = true
    e.HideBanner = true
    e.Use(middleware.CORS())

    e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
        Skipper: func(c echo.Context) bool {
            return c.Path() == "/metrics" || c.Path() == "/health"
        },
        Format: `{"time":"${time_rfc3339_nano}",${custom},"remote_ip":"${remote_ip}",` +
                `"host":"${host}","method":"${method}","uri":"${uri}","status":${status},` +
                `"error":"${error}","latency":${latency},"latency_human":"${latency_human}",` +
                `"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n",
        CustomTagFunc: func(c echo.Context, buf *bytes.Buffer) (int, error) {
            span := c.Get("span").(trace.Span)
            buf.WriteString(fmt.Sprintf("\"%s\":\"%s\"", "traceID", span.SpanContext().TraceID().String()))
            buf.WriteString(fmt.Sprintf(",\"%s\":\"%s\"", "spanID", span.SpanContext().SpanID().String()))
            return 0, nil
        },
    }))

    var tracer = otel.Tracer(config.Concurrent.FQDN + "/concurrent")
    e.Use(Tracer(tracer))

    e.Use(middleware.Recover())
    e.Logger.SetOutput(logfile)
    e.Binder = &activitypub.Binder{}

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

    apiV1R := apiV1.Group("", auth.JWT)
    apiV1R.POST("/messages", messageHandler.Post)
    apiV1R.DELETE("/messages", messageHandler.Delete)
    apiV1R.PUT("/characters", characterHandler.Put)
    apiV1R.POST("/associations", associationHandler.Post)
    apiV1R.DELETE("/associations", associationHandler.Delete)
    apiV1R.PUT("/stream", streamHandler.Put)
    apiV1R.POST("/stream/checkpoint", streamHandler.Checkpoint)
    apiV1R.PUT("/host", hostHandler.Upsert)
    apiV1R.POST("/host/hello", hostHandler.Hello)
    apiV1R.POST("/entity", entityHandler.Post)
    apiV1R.GET("/admin/sayhello/:fqdn", hostHandler.SayHello)
    apiV1R.GET("/kv/:key", userkvHandler.Get)
    apiV1R.PUT("/kv/:key", userkvHandler.Upsert)
    apiV1R.POST("/ap/entity", activitypubHandler.CreateEntity)
    apiV1R.PUT("/ap/person", activitypubHandler.UpdatePerson)

    e.GET("/*", spa)
    e.GET("/health", func(c echo.Context) (err error) {
        return c.String(http.StatusOK, "ok")
    })

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

func Tracer(tracer trace.Tracer) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            ctx, span := tracer.Start(c.Request().Context(), c.Request().Method + "-" + c.Path())
            defer span.End()
            c.Set("span", span)

            req := c.Request().WithContext(ctx)
            c.SetRequest(req)

            res := c.Response()

            res.Header().Set("trace-id", span.SpanContext().TraceID().String())
            res.Header().Set("span-id", span.SpanContext().SpanID().String())

            return next(c)
        }
    }
}

func SetupTraceProvider(endpoint string, serviceName string, serviceVersion string) (func(), error) {

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

    cleanup := func() {
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        if err := tracerProvider.Shutdown(ctx); err != nil {
            log.Printf("Failed to shutdown tracer provider: %v", err)
        }
    }
    return cleanup, nil
}

