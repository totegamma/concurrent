package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/jwt"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/exp/slices"
)

type Principal int

const (
	ISADMIN = iota
	ISLOCAL
	ISKNOWN
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

			if claims.Audience != s.config.Concurrent.FQDN {
				span.RecordError(fmt.Errorf("jwt is not for this domain"))
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "jwt is not for this domain"})
			}

			switch principal {
			case ISADMIN:
				if claims.Subject != "CC_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}

				entity, err := s.entity.Get(ctx, claims.Issuer)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not on this domain",
					})
				}

				tags := core.ParseTags(entity.Tag)

				if !tags.Has("_admin") {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not admin",
					})
				}

			case ISLOCAL:
				if claims.Subject != "CC_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}

				_, err := s.entity.Get(ctx, claims.Issuer)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not local",
					})
				}

			case ISKNOWN:

				if claims.Subject == "CC_API" { // internal user
					_, err := s.entity.Get(ctx, claims.Issuer)
					if err != nil {
						return c.JSON(http.StatusForbidden, echo.Map{
							"error":  "you are not authorized to perform this action",
							"detail": "you are not known",
						})
					}

					goto VALIDATE_OK
				}

				if claims.Subject == "CC_PASSPORT" { // external user
					_, err := s.entity.GetAddress(ctx, claims.Principal)
					if err != nil {
						return c.JSON(http.StatusForbidden, echo.Map{
							"error":  "you are not authorized to perform this action",
							"detail": "you are not known",
						})
					}

					// ckeck if domain or user is blocked

					goto VALIDATE_OK
				}

				return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})

			case ISUNITED:
				if claims.Subject != "CC_API" {
					return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid jwt"})
				}
				domain, err := s.domain.GetByCCID(ctx, claims.Issuer)
				if err != nil {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not united",
					})
				}
				domainTags := strings.Split(domain.Tag, ",")
				if slices.Contains(domainTags, "_blocked") {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
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
						"error":  "you are not authorized to perform this action",
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

func (s *service) IdentifyIdentity(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, span := tracer.Start(c.Request().Context(), "auth.IdentifyIdentity")
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

			if claims.Subject == "CC_API" {

				ccid := ""
				if isCCID(claims.Issuer) {
					ccid = claims.Issuer
				} else if isCKID(claims.Issuer) {
					ccid, err = s.ResolveSubkey(ctx, claims.Issuer)
					if err != nil {
						span.RecordError(err)
						goto skip
					}
				} else {
					span.RecordError(fmt.Errorf("invalid issuer"))
					goto skip
				}

				requester, err := s.entity.Get(ctx, ccid)
				if err != nil {
					span.RecordError(err)
					goto skip
				}

				c.Set(RequesterIdCtxKey, requester.ID)
				c.Set(RequesterTagCtxKey, requester.Tag)

			} else if claims.Subject == "CC_PASSPORT" {

				domain, err := s.domain.GetByCCID(ctx, claims.Issuer)
				if err != nil {
					span.RecordError(err)
					goto skip
				}

				ccid := claims.Principal
				if isCKID(ccid) {
					ccid, err = s.ResolveRemoteSubkey(ctx, claims.Principal, domain.ID)
					if err != nil {
						span.RecordError(err)
						goto skip
					}
				}

				// pull entity from remote if not registered
				_, err = s.entity.GetAddress(ctx, ccid)
				if err != nil {
					err = s.entity.PullEntityFromRemote(ctx, ccid, domain.ID)
					if err != nil {
						span.RecordError(err)
						goto skip
					}
				}

				c.Set(RequesterIdCtxKey, ccid)
				c.Set(RequesterDomainCtxKey, domain.ID)
			}

			span.SetAttributes(attribute.String("Issuer", claims.Issuer))
			span.SetAttributes(attribute.String("Principal", claims.Principal))

		}
	skip:
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
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

			if claims.Subject == "CC_API" {
				c.Set("requester", claims.Issuer)
			} else if claims.Subject == "CC_PASSPORT" {
				// TODO: needs to be validated
				c.Set("requester", claims.Principal)
			}

			span.SetAttributes(attribute.String("Issuer", claims.Issuer))
			span.SetAttributes(attribute.String("Audience", claims.Audience))
			span.SetAttributes(attribute.String("Principal", claims.Principal))
		}
	skip:
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}
