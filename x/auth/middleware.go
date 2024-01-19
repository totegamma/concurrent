package auth

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/core"
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
    ISNOTREGISTERED
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

			switch principal {
			case ISADMIN:
				if claims.Subject != "CC_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}

                entity, err := s.entity.Get(ctx, claims.Audience)
                if err != nil {
                    return c.JSON(http.StatusForbidden, echo.Map{
                        "error": "you are not authorized to perform this action",
                        "detail": "you are not on this domain",
                    })
                }

                tags := core.ParseTags(entity.Tag)

                if !tags.Has("_admin") {
					return c.JSON(http.StatusForbidden, echo.Map{
                        "error": "you are not authorized to perform this action",
                        "detail": "you are not admin",
                    })
                }

			case ISLOCAL:
				if claims.Subject != "CC_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}

                _, err := s.entity.Get(ctx, claims.Audience)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{
                        "error": "you are not authorized to perform this action",
                        "detail": "you are not local",
                    })
				}

			case ISKNOWN:

				if claims.Subject == "CC_API" { // internal user
                    _, err := s.entity.Get(ctx, claims.Audience)
                    if err != nil {
                        return c.JSON(http.StatusForbidden, echo.Map{
                            "error": "you are not authorized to perform this action",
                            "detail": "you are not known",
                        })
                    }

                    goto VALIDATE_OK
				}

				if claims.Subject == "CC_PASSPORT" { // external user
                    _, err := s.entity.GetAddress(ctx, claims.Audience)
                    if err != nil {
                        return c.JSON(http.StatusForbidden, echo.Map{
                            "error": "you are not authorized to perform this action",
                            "detail": "you are not known",
                        })
                    }

                    // ckeck if domain or user is blocked

                    goto VALIDATE_OK
				}

                return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})

			case ISNOTREGISTERED:
				_, err := s.entity.Get(ctx, claims.Audience)
				if err == nil {
					return c.JSON(http.StatusForbidden, echo.Map{
                        "error": "you are not authorized to perform this action",
                        "detail": "you are already known",
                    })
				}
			case ISUNITED:
				if claims.Subject != "CC_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}
				domain, err := s.domain.GetByCCID(ctx, claims.Issuer)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{
                        "error": "you are not authorized to perform this action",
                        "detail": "you are not united",
                    })
				}
				domainTags := strings.Split(domain.Tag, ",")
				if slices.Contains(domainTags, "_blocked") {
					return c.JSON(http.StatusForbidden, echo.Map{
                        "error": "you are not authorized to perform this action",
                        "detail": "you are not blocked",
                    })
				}
			case ISUNUNITED:
				if claims.Subject != "CC_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}
				_, err := s.domain.GetByCCID(ctx, claims.Issuer)
				if err == nil {
					return c.JSON(http.StatusForbidden, echo.Map{
                        "error": "you are not authorized to perform this action",
                        "detail": "you are already united",
                    })
				}
			}
            VALIDATE_OK:
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
