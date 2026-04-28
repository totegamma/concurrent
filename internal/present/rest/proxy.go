package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/totegamma/concrnt-playground/impl/interop"
	"github.com/totegamma/concrnt-playground/impl/tags"
	"github.com/totegamma/concrnt-playground/internal/domain"
)

type Proxy struct {
	services       []interop.Service
	authMiddleware echo.MiddlewareFunc
}

func NewProxy(
	services []interop.Service,
	authMiddleware echo.MiddlewareFunc,
) *Proxy {
	return &Proxy{
		services:       services,
		authMiddleware: authMiddleware,
	}
}

func (p *Proxy) RegisterRoutes(e *echo.Echo) {

	cors := echomiddleware.CORS()

	for _, service := range p.services {

		targetUrl, err := url.Parse("http://" + service.Host + ":" + strconv.Itoa(service.Port))
		if err != nil {
			panic(err)
		}
		proxy := httputil.NewSingleHostReverseProxy(targetUrl)

		proxy.Director = func(req *http.Request) {
			req.URL.Scheme = targetUrl.Scheme
			req.URL.Host = targetUrl.Host
			if service.PreservePath {
				req.URL.Path = path.Join(targetUrl.Path, req.URL.Path)
			} else {
				req.URL.Path = path.Join(targetUrl.Path, strings.TrimPrefix(req.URL.Path, service.Path))
			}

			otel.GetTextMapPropagator().Inject(req.Context(), propagation.HeaderCarrier(req.Header))
		}

		proxy.Transport = otelhttp.NewTransport(http.DefaultTransport)

		middlewares := []echo.MiddlewareFunc{
			// authService.RateLimiter(service.RateLimitConf),
		}
		if service.InjectCors {
			middlewares = append(middlewares, cors)
		}

		if !service.NoAuth {
			middlewares = append(middlewares, p.authMiddleware)
		}

		handler := func(c echo.Context) error {
			ctx := c.Request().Context()
			c.Response().Header().Set("cc-service", service.Name)

			requester, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
			if ok {
				serialized, err := json.Marshal(requester)
				if err != nil {
					return err
				}
				c.Request().Header.Set(interop.RequesterHeader, string(serialized))
			}

			tag, ok := ctx.Value(interop.RequesterTagCtxKey).(tags.Tags)
			if ok {
				tagStr := tag.ToString()
				c.Request().Header.Set(interop.RequesterTagHeader, tagStr)
			}

			proxy.ServeHTTP(c.Response(), c.Request())
			return nil
		}

		if service.Path == "/" {
			e.Any("/", handler, middlewares...)
			e.Any("/*", handler, middlewares...)
		} else {
			e.Any(service.Path, handler, middlewares...)
			e.Any(service.Path+"/*", handler, middlewares...)
		}
	}
}
