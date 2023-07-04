package activitypub

import (
	"github.com/labstack/echo/v4"
)

// ApBinder is a binder for the ActivityPub protocol.
type Binder struct{}

// NewApBinder returns a new ApBinder.
func (cb *Binder) Bind(i interface{}, c echo.Context) (err error) {
	db := new(echo.DefaultBinder)
	if c.Request().Header.Get(echo.HeaderContentType) == "application/activity+json" {
		c.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	return db.Bind(i, c)
}
