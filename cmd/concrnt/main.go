package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/xinguang/go-recaptcha"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel/trace"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/internal/infra/config"
	"github.com/concrnt/concrnt/internal/infra/database"
	"github.com/concrnt/concrnt/internal/infra/gateway"
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
				return c.Path() == "/metrics" || c.Path() == "/health"
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
			return c.Path() == "/metrics" || c.Path() == "/health" || c.Path() == "/.well-known/concrnt"
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

	cl := client.New(domainConfig.FQDN)
	cl.AddHostRemapping(domainConfig.FQDN, conf.Backends.GatewayAddr)
	cl.SetUserAgent("concrnt", version)

	moduleManager := service.NewModuleManager(rest.Endpoints, conf.Services)

	signal := service.NewSignalService(redis)
	policy := service.NewPolicyService(
		GetGlobalPolicy(),
		service.GlobalParameters{
			FQDN: domainConfig.FQDN,
		},
		cl,
	)

	serverRepo := postgres.NewServerRepository(&domainConfig, db, cl)
	serverUC := usecase.NewServerUsecase(serverRepo, &domainConfig, softwareInfo, moduleManager)

	entityRepo := postgres.NewEntityRepository(db, cl, domainConfig)
	entityUC := usecase.NewEntityUsecase(entityRepo, &domainConfig)

	recordRepo := postgres.NewRecordRepository(db)
	recordUC := usecase.NewRecordUsecase(recordRepo, &domainConfig, cl, entityUC, signal, policy)

	chunklineRepo := postgres.NewChunklineRepository(db)
	chunklineGateway := gateway.NewChunklineGateway(cl, mc, signal)
	chunklineUC := usecase.NewChunklineUsecase(chunklineRepo, chunklineGateway)

	notificationRepo := postgres.NewNotificationRepository(db)
	notificationUC := usecase.NewNotificationUsecase(notificationRepo)

	subscriber := worker.NewSubscriber(&domainConfig, cl, signal, mc)
	subscriber.Start(context.Background())

	abuseRepo := postgres.NewAbuseRepository(db)
	abuseUC := usecase.NewAbuseUsecase(abuseRepo)

	if conf.Integrations.VapidPublicKey != "" && conf.Integrations.VapidPrivateKey != "" {
		notificationReactor := worker.NewNotificationReactor(notificationUC, signal, webpush.Options{
			Subscriber:      "mailto:admin@" + domainConfig.FQDN,
			VAPIDPublicKey:  conf.Integrations.VapidPublicKey,
			VAPIDPrivateKey: conf.Integrations.VapidPrivateKey,
			TTL:             30,
		})
		notificationReactor.Start(context.Background())
	}

	authMiddleware := middleware.NewAuthMiddleware(domainConfig, cl, serverUC, entityRepo)

	meta := conf.Meta
	meta["captchaSiteKey"] = conf.Integrations.CaptchaSitekey
	meta["vapidKey"] = conf.Integrations.VapidPublicKey
	meta["registration"] = conf.Concrnt.Registration

	wellKnownHandler := rest.NewWellKnownHandler(serverUC, meta)

	wellKnownHandler.RegisterRoutes(e)

	apiHandler := rest.NewHandler(
		domainConfig,
		recordUC,
		chunklineUC,
		serverUC,
		entityUC,
		notificationUC,
		abuseUC,
		signal,
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

	apiHandler.RegisterRoutes(api)

	proxy := rest.NewProxy(conf.Services, authMiddleware.IdentifyIdentity)
	proxy.RegisterRoutes(e)

	e.GET("/health", func(c echo.Context) (err error) {
		// ctx := c.Request().Context()

		/*
			err = sqlDB.Ping()
			if err != nil {
				return c.String(http.StatusInternalServerError, "db error")
			}

			err = rdb.Ping(ctx).Err()
			if err != nil {
				return c.String(http.StatusInternalServerError, "redis error")
			}
		*/

		return c.String(200, "ok")
	})

	e.Logger.Fatal(e.Start(":8000"))

}
