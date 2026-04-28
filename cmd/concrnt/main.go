package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"github.com/xinguang/go-recaptcha"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel/trace"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/client"
	"github.com/totegamma/concrnt-playground/internal/infra/config"
	"github.com/totegamma/concrnt-playground/internal/infra/database"
	"github.com/totegamma/concrnt-playground/internal/infra/gateway"
	"github.com/totegamma/concrnt-playground/internal/infra/repository"
	"github.com/totegamma/concrnt-playground/internal/present/rest"
	"github.com/totegamma/concrnt-playground/internal/present/rest/middleware"
	"github.com/totegamma/concrnt-playground/internal/service"
	"github.com/totegamma/concrnt-playground/internal/usecase"
	"github.com/totegamma/concrnt-playground/internal/utils"
	"github.com/totegamma/concrnt-playground/internal/worker"
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

	conf, err := config.Load("/etc/concrnt/config/config.yaml")
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	globalConfig := conf.GlobalConfig()

	log.Printf("Concrnt %s starting...", version)
	log.Printf("Config loaded! I am: %s @ %s on %s", globalConfig.CCID, globalConfig.FQDN, globalConfig.Layer)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(echomiddleware.Recover())

	if conf.Server.EnableTrace {
		cleanup, err := utils.SetupTraceProvider(conf.Server.TraceEndpoint, conf.NodeInfo.FQDN+"/ccapi", version)
		if err != nil {
			panic(err)
		}
		defer cleanup()

		skipper := otelecho.WithSkipper(
			func(c echo.Context) bool {
				return c.Path() == "/metrics" || c.Path() == "/health"
			},
		)
		e.Use(otelecho.Middleware(conf.NodeInfo.FQDN, skipper))

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

	db, err := database.NewPostgres(conf.Server.PostgresDsn)
	if err != nil {
		panic("failed to connect database")
	}

	err = database.MigratePostgres(db)
	if err != nil {
		panic("failed to migrate database")
	}

	mc := database.NewMemcached(conf.Server.MemcachedAddr)
	defer mc.Close()

	redis := database.NewRedis(conf.Server.RedisAddr, "", conf.Server.RedisDB)

	cl := client.New(conf.NodeInfo.FQDN)
	cl.AddHostRemapping(conf.NodeInfo.FQDN, conf.Server.GatewayAddr)
	cl.SetUserAgent("concrnt", version)

	moduleManager := service.NewModuleManager(rest.Endpoints, conf.Services)

	signal := service.NewSignalService(redis)
	policy := service.NewPolicyService(GetGlobalPolicy(), cl)

	serverRepo := repository.NewServerRepository(&globalConfig, db, cl)
	serverUC := usecase.NewServerUsecase(serverRepo, &globalConfig, softwareInfo, moduleManager)

	entityRepo := repository.NewEntityRepository(db, cl, globalConfig)
	entityUC := usecase.NewEntityUsecase(entityRepo, &globalConfig)

	recordRepo := repository.NewRecordRepository(db)
	recordUC := usecase.NewRecordUsecase(recordRepo, &globalConfig, cl, entityUC, signal, policy)

	chunklineRepo := repository.NewChunklineRepository(db)
	chunklineGateway := gateway.NewChunklineGateway(cl)
	chunklineUC := usecase.NewChunklineUsecase(chunklineRepo, chunklineGateway)

	notificationRepo := repository.NewNotificationRepository(db)
	notificationUC := usecase.NewNotificationUsecase(notificationRepo)

	subscriber := worker.NewSubscriber(&globalConfig, cl, signal)
	subscriber.Start(context.Background())

	abuseRepo := repository.NewAbuseRepository(db)
	abuseService := service.NewAbuseService(abuseRepo)

	if conf.Server.VapidPublicKey != "" && conf.Server.VapidPrivateKey != "" {
		notificationReactor := worker.NewNotificationReactor(notificationUC, signal, webpush.Options{
			Subscriber:      "mailto:admin@" + globalConfig.FQDN,
			VAPIDPublicKey:  conf.Server.VapidPublicKey,
			VAPIDPrivateKey: conf.Server.VapidPrivateKey,
			TTL:             30,
		})
		notificationReactor.Start(context.Background())
	}

	authMiddleware := middleware.NewAuthMiddleware(globalConfig, cl, serverUC, entityRepo)

	wellKnownHandler := rest.NewWellKnownHandler(serverUC)
	wellKnownHandler.RegisterRoutes(e)

	apiHandler := rest.NewHandler(
		globalConfig,
		recordUC,
		chunklineUC,
		serverUC,
		entityUC,
		notificationUC,
		abuseService,
		signal,
		moduleManager,
	)
	api := e.Group("", authMiddleware.IdentifyIdentity, authMiddleware.IdentifyIdentity)

	if conf.Server.CaptchaSecret != "" {
		validator, err := recaptcha.NewWithSecert(conf.Server.CaptchaSecret)
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
