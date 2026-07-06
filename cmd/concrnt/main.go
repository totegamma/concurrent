package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/xinguang/go-recaptcha"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel/trace"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/infra/cluster"
	"github.com/concrnt/concrnt/internal/infra/config"
	"github.com/concrnt/concrnt/internal/infra/database"
	"github.com/concrnt/concrnt/internal/infra/gateway"
	"github.com/concrnt/concrnt/internal/infra/jobqueue"
	"github.com/concrnt/concrnt/internal/infra/pubsub"
	"github.com/concrnt/concrnt/internal/infra/repository/postgres"
	"github.com/concrnt/concrnt/internal/present/rest"
	"github.com/concrnt/concrnt/internal/present/rest/middleware"
	"github.com/concrnt/concrnt/internal/service"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/concrnt/concrnt/internal/utils"
	"github.com/concrnt/concrnt/internal/worker"
)

var (
	version      = "unknown"
	buildMachine = "unknown"
	buildTime    = "unknown"
	goVersion    = "unknown"
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

func main() {

	lh := &CustomHandler{Handler: slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})}
	slogger := slog.New(lh)
	slog.SetDefault(slogger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprint(os.Stderr, concrnt.Banner)

	configPath := os.Getenv("CONCRNT_CONFIG")
	if configPath == "" {
		configPath = "/etc/concrnt/config"
	}

	conf, err := config.Load(configPath)
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	domainConfig := conf.DomainConfig()

	slog.Info("concrnt starting", slog.String("version", version))
	slog.Info(
		"config loaded",
		slog.String("csid", domainConfig.CSID),
		slog.String("fqdn", domainConfig.FQDN),
		slog.String("layer", domainConfig.Layer),
	)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(echomiddleware.Recover())

	if conf.Observability.EnableTrace {
		cleanup, err := utils.SetupTraceProvider(conf.Observability.TraceEndpoint, conf.Concrnt.FQDN+"/ccapi", version)
		if err != nil {
			panic(err)
		}
		defer cleanup()

		skipper := otelecho.WithSkipper(
			func(c echo.Context) bool {
				return c.Path() == "/metrics" || c.Path() == "/health" || c.Path() == "/ready"
			},
		)
		e.Use(otelecho.Middleware(conf.Concrnt.FQDN, skipper))

		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				span := trace.SpanFromContext(c.Request().Context())
				c.Response().Header().Set("trace-id", span.SpanContext().TraceID().String())
				return next(c)
			}
		})
	}

	e.Use(echomiddleware.LoggerWithConfig(echomiddleware.LoggerConfig{
		Skipper: func(c echo.Context) bool {
			return c.Path() == "/metrics" || c.Path() == "/health" || c.Path() == "/ready" || c.Path() == "/.well-known/concrnt"
		},
		Format: `{"time":"${time_rfc3339_nano}",${custom},"remote_ip":"${remote_ip}",` +
			`"host":"${host}","method":"${method}","uri":"${uri}","status":${status},` +
			`"error":"${error}","latency":${latency},"latency_human":"${latency_human}",` +
			`"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n",
		CustomTagFunc: func(c echo.Context, buf *bytes.Buffer) (int, error) {
			span := trace.SpanFromContext(c.Request().Context())
			fmt.Fprintf(buf, "\"%s\":\"%s\"", "traceID", span.SpanContext().TraceID().String())
			fmt.Fprintf(buf, ",\"%s\":\"%s\"", "spanID", span.SpanContext().SpanID().String())
			return 0, nil
		},
	}))

	softwareInfo := concrnt.SoftwareInfo{
		Version:      version,
		BuildMachine: buildMachine,
		BuildTime:    buildTime,
		GoVersion:    goVersion,
	}

	db, err := database.NewPostgres(conf.Backends.PostgresDsn)
	if err != nil {
		panic("failed to connect database")
	}

	err = database.MigratePostgres(db)
	if err != nil {
		panic("failed to migrate database")
	}

	mc := database.NewMemcached(conf.Backends.MemcachedAddr)
	defer mc.Close()

	redis := database.NewRedis(conf.Backends.RedisAddr, "", conf.Backends.RedisDB)

	clusterConf := conf.Concrnt.Cluster
	internalPort := clusterConf.InternalPort
	if internalPort == 0 {
		internalPort = 8001
	}

	clustered := clusterConf.ElectorEndpoint != ""

	var elector cluster.Elector
	var discovery worker.PeerDiscovery // nil in standalone mode: no peers to poll
	if clustered {
		httpElector := cluster.NewHTTPElector(clusterConf.ElectorEndpoint)
		elector = httpElector
		discovery = httpElector
	} else {
		elector = cluster.AlwaysLeader{}
	}

	cl := client.New(domainConfig.FQDN)
	cl.AddHostRemapping(domainConfig.FQDN, conf.Backends.GatewayAddr)
	cl.SetUserAgent("concrnt", version)

	moduleManager := service.NewModuleManager(rest.Endpoints, conf.Services)

	redisPubsub := pubsub.NewRedisPubsub(redis)
	deliveryQueue := jobqueue.NewRedisDeliveryQueue(redis)
	policy := service.NewPolicyService(
		GetGlobalPolicy(),
		service.GlobalParameters{
			FQDN: domainConfig.FQDN,
		},
		cl,
	)

	serverRepo := postgres.NewServerRepository(&domainConfig, db, cl)
	serverUC := usecase.NewServerUsecase(serverRepo, &domainConfig, softwareInfo, moduleManager)

	residenceRepo := postgres.NewResidenceRepository(db, cl, domainConfig)
	recordRepo := postgres.NewRecordRepository(db)
	recordUC := usecase.NewRecordUsecase(recordRepo, residenceRepo, &domainConfig, cl, redisPubsub, policy, deliveryQueue)
	residenceUC := usecase.NewResidenceUsecase(residenceRepo, recordUC, &domainConfig)

	chunklineRepo := postgres.NewChunklineRepository(db)

	notificationRepo := postgres.NewNotificationRepository(db)
	notificationUC := usecase.NewNotificationUsecase(notificationRepo)

	leaderSub := worker.NewLeaderSubscriber(&domainConfig, cl, redisPubsub, discovery)
	workerSub := worker.NewWorkerSubscriber(elector)
	subscriber := worker.NewSubscriberManager(elector, leaderSub, workerSub)

	subscriptionUC := usecase.NewSubscriptionUsecase(subscriber, redisPubsub)
	leaderSub.RegisterClient(subscriptionUC)

	chunklineGateway := gateway.NewChunklineGateway(cl, mc, subscriber, redisPubsub)
	chunklineUC := usecase.NewChunklineUsecase(chunklineRepo, chunklineGateway)

	// the delivery queue is a redis-streams consumer group: safe (and useful)
	// to run on every replica
	deliveryWorker := worker.NewDeliveryWorker(&domainConfig, cl, redisPubsub, recordUC, deliveryQueue)
	deliveryWorker.Start(ctx)

	abuseRepo := postgres.NewAbuseRepository(db)
	abuseUC := usecase.NewAbuseUsecase(abuseRepo)

	var notificationReactor *worker.NotificationReactor
	if conf.Integrations.VapidPublicKey != "" && conf.Integrations.VapidPrivateKey != "" {
		// cross-replica push dedup only matters when a leadership handover can
		// overlap two reactors; standalone deployments skip the redis round
		// trip per notification
		var notificationDeduper worker.NotificationDeduper
		if clustered {
			notificationDeduper = pubsub.NewRedisDeduper(redis)
		}
		notificationReactor = worker.NewNotificationReactor(notificationUC, subscriptionUC, notificationDeduper, webpush.Options{
			Subscriber:      "mailto:admin@" + domainConfig.FQDN,
			VAPIDPublicKey:  conf.Integrations.VapidPublicKey,
			VAPIDPrivateKey: conf.Integrations.VapidPrivateKey,
			TTL:             30,
		})
	}

	// singleton workers run only while this replica holds the leadership: the
	// federation subscriber (one upstream websocket per remote host for the
	// whole cluster), the chunkline cache updater, and the push reactor
	go elector.Run(ctx, func(leadCtx context.Context) {
		leaderSub.Start(leadCtx)
		chunklineGateway.StartWorker(leadCtx, leaderSub)
		if notificationReactor != nil {
			notificationReactor.Start(leadCtx)
		}
	})

	if clustered {
		internalHandler := rest.NewInternalHandler(subscriptionUC, leaderSub, leaderSub, elector)
		rest.StartInternalListener(ctx, fmt.Sprintf(":%d", internalPort), internalHandler)
	}

	authMiddleware := middleware.NewAuthMiddleware(domainConfig, cl, serverUC, recordUC)

	meta := conf.Meta
	meta["captchaSiteKey"] = conf.Integrations.CaptchaSitekey
	meta["vapidKey"] = conf.Integrations.VapidPublicKey
	meta["registration"] = conf.Concrnt.Registration

	wellKnownHandler := rest.NewWellKnownHandler(serverUC, meta)

	wellKnownHandler.RegisterRoutes(e)

	apiHandler := rest.NewHandler(
		domainConfig,
		residenceUC,
		recordUC,
		chunklineUC,
		serverUC,
		notificationUC,
		abuseUC,
		subscriptionUC,
		moduleManager,
	)
	api := e.Group("", authMiddleware.IdentifyIdentity, authMiddleware.IdentifyIdentity)

	if conf.Integrations.CaptchaSecret != "" {
		validator, err := recaptcha.NewWithSecert(conf.Integrations.CaptchaSecret)
		if err != nil {
			panic("failed to initialize recaptcha: " + err.Error())
		}
		captchaMiddleware := middleware.Recaptcha(validator)
		api.Use(captchaMiddleware)
	}

	apiHandler.RegisterRoutes(e, api)

	proxy := rest.NewProxy(conf.Services, authMiddleware.IdentifyIdentity)
	proxy.RegisterRoutes(e)

	e.GET("/tos", func(c echo.Context) (err error) {
		return c.File("/etc/concrnt/static/tos.txt")
	})
	e.OPTIONS("/tos", handleNop)

	e.GET("/code-of-conduct", func(c echo.Context) (err error) {
		return c.File("/etc/concrnt/static/code-of-conduct.txt")
	})
	e.OPTIONS("/code-of-conduct", handleNop)

	e.GET("/register-template", func(c echo.Context) (err error) {
		return c.File("/etc/concrnt/static/register-template.json")
	})
	e.OPTIONS("/register-template", handleNop)

	e.GET("/health", func(c echo.Context) (err error) {
		return c.String(http.StatusOK, "ok")
	})

	var ready atomic.Bool
	ready.Store(true)

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}

	e.GET("/ready", func(c echo.Context) (err error) {
		if !ready.Load() {
			return c.String(http.StatusServiceUnavailable, "shutting down")
		}

		// only Postgres gates readiness: redis and memcached are soft
		// dependencies (cache misses fall back to origin, publish failures
		// are logged), and failing all replicas at once on a cache-tier blip
		// would turn a degradation into a full outage
		if err := sqlDB.PingContext(c.Request().Context()); err != nil {
			return c.String(http.StatusServiceUnavailable, "db error")
		}

		return c.String(http.StatusOK, "ok")
	})

	var serverFailed atomic.Bool
	go func() {
		if err := e.Start(":8000"); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly", slog.String("error", err.Error()))
			serverFailed.Store(true)
			stop()
		}
	}()

	<-ctx.Done()

	slog.Info("shutting down")
	ready.Store(false)

	if clustered {
		// keep serving briefly so the endpoint controller stops routing to
		// this pod before connections are closed
		time.Sleep(3 * time.Second)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()

	if err := e.Shutdown(shutdownCtx); err != nil {
		slog.Error("failed to shut down gracefully", slog.String("error", err.Error()))
	}

	// Shutdown does not touch hijacked connections: this is what terminates
	// the realtime websockets so clients reconnect to another replica
	e.Close()

	if serverFailed.Load() {
		// e.g. the listen address was already bound: exit non-zero so
		// supervisors keying on exit status restart the process
		os.Exit(1)
	}
}

func handleNop(c echo.Context) error {
	return c.NoContent(http.StatusOK)
}
