package auth

import (
    "fmt"
    "net/http"
    "strings"

    "github.com/labstack/echo/v4"
    "github.com/totegamma/concurrent/x/util"
)

// JWT is middleware which validate jwt
func JWT(next echo.HandlerFunc) echo.HandlerFunc {
    return func (c echo.Context) error {
        _, span := tracer.Start(c.Request().Context(), "auth.JWT")
        defer span.End()
        authInfo := c.Request().Header.Get("Authentication")
        split := strings.Split(authInfo, " ")
        if len(split) != 2 {
            span.RecordError(fmt.Errorf("invalid authentication header"))
            return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid authentication header"})
        }
        authType, jwt := split[0], split[1]
        if authType != "Bearer" {
            span.RecordError(fmt.Errorf("only Bearer is acceptable"))
            return c.JSON(http.StatusUnauthorized, echo.Map{"error": "only Bearer is acceptable"})
        }

        claims, err := util.ValidateJWT(jwt)
        if err != nil {
            span.RecordError(err)
            return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
        }

        c.Set("jwtclaims", claims)

        return next(c)
    }
}

