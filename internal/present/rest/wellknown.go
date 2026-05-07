package rest

import (
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"

	"github.com/concrnt/concrnt/internal/present/rest/presenter"
	"github.com/concrnt/concrnt/internal/usecase"
)

type WellKnownHandler struct {
	server *usecase.ServerUsecase
	meta   map[string]any
}

func NewWellKnownHandler(
	server *usecase.ServerUsecase,
	meta map[string]any,
) *WellKnownHandler {
	return &WellKnownHandler{
		server: server,
		meta:   meta,
	}
}

func (p *WellKnownHandler) RegisterRoutes(e *echo.Echo) {
	w := e.Group("", echomiddleware.CORS())
	w.GET("/.well-known/concrnt", p.handleWellKnown)
}

func (p *WellKnownHandler) handleWellKnown(c echo.Context) error {
	server, err := p.server.GetThisServer()
	if err != nil {
		return presenter.InternalError(c, err)
	}

	wellknown := server.WellKnown
	wellknown.Meta = p.meta

	return presenter.OK(c, wellknown)
}
