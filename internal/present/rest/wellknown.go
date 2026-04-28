package rest

import (
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"

	"github.com/totegamma/concrnt-playground/internal/present/rest/presenter"
	"github.com/totegamma/concrnt-playground/internal/usecase"
)

type WellKnownHandler struct {
	server *usecase.ServerUsecase
}

func NewWellKnownHandler(
	server *usecase.ServerUsecase,
) *WellKnownHandler {
	return &WellKnownHandler{
		server: server,
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

	return presenter.OK(c, server.WellKnown)
}
