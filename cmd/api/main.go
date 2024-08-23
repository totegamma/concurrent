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

	"github.com/totegamma/concurrent"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/ack"
	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/job"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/profile"
	"github.com/totegamma/concurrent/x/store"
	"github.com/totegamma/concurrent/x/subscription"
	"github.com/totegamma/concurrent/x/timeline"
	"github.com/totegamma/concurrent/x/userkv"

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

	fmt.Fprint(os.Stderr, concurrent.Banner)

	handler := &CustomHandler{Handler: slog.NewJSONHandler(os.Stdout, nil)}
	slogger := slog.New(handler)
	slog.SetDefault(slogger)

	slog.Info(fmt.Sprintf("Concrnt %s starting...", version))

	e := echo.New()
	e.HidePort = true
	e.HideBanner = true
	config := Config{}
	configPath := os.Getenv("CONCRNT_CONFIG")
	if configPath == "" {
		configPath = "/etc/concrnt/config/config.yaml"
	}

	err := config.Load(configPath)
	if err != nil {
		slog.Error("Failed to load config: ", err)
	}

	conconf := core.SetupConfig(config.Concrnt)

	slog.Info(fmt.Sprintf("Config loaded! I am: %s", conconf.CCID))

	if config.Server.EnableTrace {
		cleanup, err := setupTraceProvider(config.Server.TraceEndpoint, config.Concrnt.FQDN+"/ccapi", version)
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
		Logger:         gormLogger,
		TranslateError: true,
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

	// !! migration from < v1.2.0
	err = db.Exec("ALTER TABLE \"associations\" DROP CONSTRAINT IF EXISTS \"idx_associations_unique\"").Error
	if err != nil {
		panic("failed to drop constraint: " + err.Error())
	}

	// Migrate the schema
	slog.Info("start migrate")
	err = db.AutoMigrate(
		&core.Schema{},
		&core.Message{},
		&core.Profile{},
		&core.Association{},
		&core.Timeline{},
		&core.TimelineItem{},
		&core.Domain{},
		&core.Entity{},
		&core.EntityMeta{},
		&core.Ack{},
		&core.Key{},
		&core.UserKV{},
		&core.Subscription{},
		&core.SubscriptionItem{},
		&core.SemanticID{},
		&core.Job{},
	)

	if err != nil {
		panic("failed to migrate schema: " + err.Error())
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     config.Server.RedisAddr,
		Password: "", // no password set
		DB:       config.Server.RedisDB,
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
	defer mc.Close()

	client := client.NewClient()

	globalPolicy := concurrent.GetDefaultGlobalPolicy()

	policy := concurrent.SetupPolicyService(rdb, globalPolicy, conconf)
	agent := concurrent.SetupAgent(db, rdb, mc, client, policy, conconf, config.Server.RepositoryPath)

	domainService := concurrent.SetupDomainService(db, client, conconf)
	domainHandler := domain.NewHandler(domainService)

	userKvService := concurrent.SetupUserkvService(db)
	userkvHandler := userkv.NewHandler(userKvService)

	messageService := concurrent.SetupMessageService(db, rdb, mc, client, policy, conconf)
	messageHandler := message.NewHandler(messageService)

	associationService := concurrent.SetupAssociationService(db, rdb, mc, client, policy, conconf)
	associationHandler := association.NewHandler(associationService)

	profileService := concurrent.SetupProfileService(db, rdb, mc, client, policy, conconf)
	profileHandler := profile.NewHandler(profileService)

	timelineService := concurrent.SetupTimelineService(db, rdb, mc, client, policy, conconf)
	timelineHandler := timeline.NewHandler(timelineService)

	entityService := concurrent.SetupEntityService(db, rdb, mc, client, policy, conconf)
	entityHandler := entity.NewHandler(entityService)

	authService := concurrent.SetupAuthService(db, rdb, mc, client, policy, conconf)
	authHandler := auth.NewHandler(authService)

	keyService := concurrent.SetupKeyService(db, rdb, mc, client, conconf)
	keyHandler := key.NewHandler(keyService)

	ackService := concurrent.SetupAckService(db, rdb, mc, client, policy, conconf)
	ackHandler := ack.NewHandler(ackService)

	storeService := concurrent.SetupStoreService(db, rdb, mc, client, policy, conconf, config.Server.RepositoryPath)
	storeHandler := store.NewHandler(storeService)

	subscriptionService := concurrent.SetupSubscriptionService(db, rdb, mc, client, policy, conconf)
	subscriptionHandler := subscription.NewHandler(subscriptionService)

	jobService := concurrent.SetupJobService(db)
	jobHandler := job.NewHandler(jobService)

	apiV1 := e.Group("", auth.ReceiveGatewayAuthPropagation)
	// store
	apiV1.POST("/commit", storeHandler.Commit)

	// domain
	apiV1.GET("/domain", func(c echo.Context) error {
		meta := config.Profile
		meta.Registration = config.Concrnt.Registration
		meta.Version = version
		meta.BuildInfo = BuildInfo{
			BuildTime:    buildTime,
			BuildMachine: buildMachine,
			GoVersion:    goVersion,
		}
		meta.SiteKey = config.Server.CaptchaSitekey

		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": core.Domain{
			ID:        conconf.FQDN,
			CCID:      conconf.CCID,
			Dimension: conconf.Dimension,
			Meta:      meta,
		}})
	})
	apiV1.GET("/domain/:id", domainHandler.Get)
	apiV1.GET("/domains", domainHandler.List)

	// entity
	apiV1.GET("/entity", entityHandler.GetSelf, auth.Restrict(auth.ISREGISTERED))
	apiV1.GET("/entity/:id", entityHandler.Get)
	apiV1.GET("/entity/:id/acking", ackHandler.GetAcking)
	apiV1.GET("/entity/:id/acker", ackHandler.GetAcker)
	apiV1.GET("/entities", entityHandler.List)

	// message
	apiV1.GET("/message/:id", messageHandler.Get)
	apiV1.GET("/message/:id/associations", associationHandler.GetFiltered)
	apiV1.GET("/message/:id/associationcounts", associationHandler.GetCounts)
	apiV1.GET("/message/:id/associations/mine", associationHandler.GetOwnByTarget, auth.Restrict(auth.ISKNOWN))

	// association
	apiV1.GET("/association/:id", associationHandler.Get)

	// profile
	apiV1.GET("/profile/:id", profileHandler.Get)
	apiV1.GET("/profile/:owner/:semanticid", profileHandler.GetBySemanticID)
	apiV1.GET("/profiles", profileHandler.Query)

	// timeline
	apiV1.GET("/timeline/:id", timelineHandler.Get)
	apiV1.GET("/timeline/:id/query", timelineHandler.Query)
	apiV1.GET("/timelines", timelineHandler.List)
	apiV1.GET("/timelines/mine", timelineHandler.ListMine)
	apiV1.GET("/timelines/recent", timelineHandler.Recent)
	apiV1.GET("/timelines/range", timelineHandler.Range)
	apiV1.GET("/timelines/chunks", timelineHandler.GetChunks)
	apiV1.GET("/timelines/realtime", timelineHandler.Realtime)

	// userkv
	apiV1.GET("/kv/:key", userkvHandler.Get, auth.Restrict(auth.ISREGISTERED))
	apiV1.PUT("/kv/:key", userkvHandler.Upsert, auth.Restrict(auth.ISREGISTERED))

	// auth
	apiV1.GET("/auth/passport", authHandler.GetPassport, auth.Restrict(auth.ISLOCAL))

	// key
	apiV1.GET("/key/:id", keyHandler.GetKeyResolution)
	apiV1.GET("/keys/mine", keyHandler.GetKeyMine, auth.Restrict(auth.ISREGISTERED))

	// subscription
	apiV1.GET("/subscription/:id", subscriptionHandler.GetSubscription)
	apiV1.GET("/subscriptions/mine", subscriptionHandler.GetOwnSubscriptions, auth.Restrict(auth.ISLOCAL))

	// storage
	apiV1.GET("/repository", storeHandler.Get, auth.Restrict(auth.ISREGISTERED))
	apiV1.POST("/repository", storeHandler.Post, auth.Restrict(auth.ISLOCAL))

	// job
	apiV1.GET("/jobs", jobHandler.List, auth.Restrict(auth.ISREGISTERED))
	apiV1.POST("/jobs", jobHandler.Create, auth.Restrict(auth.ISREGISTERED))
	apiV1.DELETE("/job/:id", jobHandler.Cancel, auth.Restrict(auth.ISREGISTERED))

	// misc
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

	var timelineSubscriptionMetrics = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cc_timeline_subscriptions",
			Help: "timeline subscriptions",
		},
		[]string{"timeline"},
	)
	prometheus.MustRegister(timelineSubscriptionMetrics)

	var resourceCountMetrics = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cc_resources_count",
			Help: "resources count",
		},
		[]string{"type"},
	)
	prometheus.MustRegister(resourceCountMetrics)

	var timelineRealtimeConnectionMetrics = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "cc_timeline_realtime_connections",
			Help: "timeline realtime connections",
		},
	)
	prometheus.MustRegister(timelineRealtimeConnectionMetrics)

	go func() {
		for {
			time.Sleep(15 * time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			subscriptions, err := timelineService.ListTimelineSubscriptions(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to list timeline subscriptions: %v", err))
				continue
			}
			for timeline, count := range subscriptions {
				timelineSubscriptionMetrics.WithLabelValues(timeline).Set(float64(count))
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

			count, err = profileService.Count(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to count profiles: %v", err))
				continue
			}
			resourceCountMetrics.WithLabelValues("profile").Set(float64(count))

			count, err = associationService.Count(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to count associations: %v", err))
				continue
			}
			resourceCountMetrics.WithLabelValues("association").Set(float64(count))

			count, err = timelineService.Count(ctx)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to count timelines: %v", err))
				continue
			}
			resourceCountMetrics.WithLabelValues("timeline").Set(float64(count))

			count = timelineService.CurrentRealtimeConnectionCount()
			timelineRealtimeConnectionMetrics.Set(float64(count))
		}
	}()

	e.GET("/metrics", echoprometheus.NewHandler())

	agent.Boot()

	port := ":8000"
	envport := os.Getenv("CC_API_PORT")
	if envport != "" {
		port = ":" + envport
	}
	e.Logger.Fatal(e.Start(port))
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
