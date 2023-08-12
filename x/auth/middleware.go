package auth

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/util"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/exp/slices"
	"net/http"
	"strings"
)

type Principal int

const (
	ISADMIN = iota
	ISLOCAL
	ISKNOWN
	ISUNKNOWN
	ISUNITED
	ISUNUNITED
)

func (s *Service) Restrict(principal Principal) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, span := tracer.Start(c.Request().Context(), "auth.Restrict")
			defer span.End()
			claims, ok := c.Get("jwtclaims").(util.JwtClaims)
			if !ok {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid authentication header"})
			}

			switch principal {
			case ISADMIN:
				if claims.Subject != "CONCURRENT_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}
				if !slices.Contains(s.config.Concurrent.Admins, claims.Audience) {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are not admin"})
				}
			case ISLOCAL:
				if claims.Subject != "CONCURRENT_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}

				ent, err := s.entity.Get(ctx, claims.Audience)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are not known"})
				}

				if ent.Domain != "" {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are not local"})
				}
			case ISKNOWN:
				if claims.Subject != "CONCURRENT_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}
				_, err := s.entity.Get(ctx, claims.Audience)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are not known"})
				}
			case ISUNKNOWN:
				_, err := s.entity.Get(ctx, claims.Audience)
				if err == nil {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are already known"})
				}
			case ISUNITED:
				if claims.Subject != "CONCURRENT_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}
				_, err := s.domain.GetByCCID(ctx, claims.Issuer)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are not united"})
				}
			case ISUNUNITED:
				if claims.Subject != "CONCURRENT_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}
				_, err := s.domain.GetByCCID(ctx, claims.Issuer)
				if err == nil {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are already united"})
				}
			}
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// JWT is middleware which validate jwt
func JWT(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, span := tracer.Start(c.Request().Context(), "auth.JWT")
		defer span.End()
		authInfo := c.Request().Header.Get("authorization")
		if authInfo == "" { // XXX for backward compatibility
			authInfo = c.Request().Header.Get("Authentication")
		}

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
		span.SetAttributes(attribute.String("Audience", claims.Audience))

		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}
