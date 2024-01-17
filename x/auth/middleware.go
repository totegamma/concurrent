package auth

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/jwt"
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

// Restrict is a middleware that restricts access to certain routes
func (s *service) Restrict(principal Principal) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, span := tracer.Start(c.Request().Context(), "auth.Restrict")
			defer span.End()
			claims, ok := c.Get("jwtclaims").(jwt.Claims)
			if !ok {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid authentication header"})
			}
			tags := strings.Split(claims.Tag, ",")

			switch principal {
			case ISADMIN:
				if claims.Subject != "CONCURRENT_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}
				if !slices.Contains(tags, "_admin") {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are not admin"})
				}
			case ISLOCAL:
				if claims.Subject != "CONCURRENT_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}

                _, err := s.entity.Get(ctx, claims.Audience)
				if err != nil {
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

				// remote user must be checked if it's domain is not blocked
				if claims.Issuer != s.config.Concurrent.CCID {
					domain, err := s.domain.GetByCCID(ctx, claims.Issuer)
					if err != nil {
						return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "your domain is not known"})
					}
					domainTags := strings.Split(domain.Tag, ",")
					if slices.Contains(domainTags, "_blocked") {
						return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "your domain is blocked"})
					}
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
				domain, err := s.domain.GetByCCID(ctx, claims.Issuer)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are not united"})
				}
				domainTags := strings.Split(domain.Tag, ",")
				if slices.Contains(domainTags, "_blocked") {
					return c.JSON(http.StatusForbidden, echo.Map{"error": "you are not authorized to perform this action", "detail": "you are not blocked"})
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

// ParseJWT is middleware which validate jwt
// ignore if jwt is missing
// error only if jwt is invalid
func ParseJWT(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, span := tracer.Start(c.Request().Context(), "auth.ParseJWT")
		defer span.End()

		authHeader := c.Request().Header.Get("authorization")

		if authHeader != "" {
			split := strings.Split(authHeader, " ")
			if len(split) != 2 {
				span.RecordError(fmt.Errorf("invalid authentication header"))
				goto skip
			}
			authType, token := split[0], split[1]
			if authType != "Bearer" {
				span.RecordError(fmt.Errorf("only Bearer is acceptable"))
				goto skip
			}

			claims, err := jwt.Validate(token)
			if err != nil {
				span.RecordError(err)
				goto skip
			}

			c.Set("jwtclaims", claims)
			span.SetAttributes(attribute.String("Audience", claims.Audience))
		}
	skip:

		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}
