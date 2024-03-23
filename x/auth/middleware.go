package auth

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key"
	"go.opentelemetry.io/otel/attribute"
)

type Principal int

const (
	ISADMIN = iota
	ISLOCAL
	ISKNOWN
	ISUNITED
)

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

			if claims.Audience != s.config.Concurrent.FQDN {
				span.RecordError(fmt.Errorf("jwt is not for this domain"))
				goto skip
			}

			c.Set("jwtclaims", claims)

			if claims.Subject == "CC_API" {

				ccid := ""
				if key.IsCCID(claims.Issuer) {
					ccid = claims.Issuer
				} else if key.IsCKID(claims.Issuer) {
					ccid, err = s.key.ResolveSubkey(ctx, claims.Issuer)
					if err != nil {
						span.RecordError(err)
						goto skip
					}
				} else {
					span.RecordError(fmt.Errorf("invalid issuer"))
					goto skip
				}

				var requester core.Entity
				var domain core.Domain
				var err error
				tags := &core.Tags{}

				requester, err = s.entity.Get(ctx, ccid)
				if err == nil {
					tags = core.ParseTags(requester.Tag)
					c.Set(core.RequesterTypeCtxKey, core.LocalUser)
					c.Set(core.RequesterTagCtxKey, tags)
					span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.LocalUser)))
					span.SetAttributes(attribute.String("RequesterTag", requester.Tag))
				} else {
					domain, err = s.domain.GetByCCID(ctx, claims.Issuer)
					if err != nil {
						span.RecordError(err)
						goto skip
					}
					tags = core.ParseTags(domain.Tag)
					c.Set(core.RequesterTypeCtxKey, core.RemoteDomain)
					c.Set(core.RequesterDomainCtxKey, domain.ID)
					c.Set(core.RequesterDomainTagsKey, tags)
					span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.RemoteDomain)))
					span.SetAttributes(attribute.String("RequesterDomain", domain.ID))
					span.SetAttributes(attribute.String("RequesterDomainTags", domain.Tag))
				}

				c.Set(core.RequesterIdCtxKey, requester.ID)
				span.SetAttributes(attribute.String("RequesterId", ccid))

				if tags.Has("_block") {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are blocked",
					})
				}

			} else if claims.Subject == "CC_PASSPORT" {

				domain, err := s.domain.GetByCCID(ctx, claims.Issuer)
				if err != nil {
					if claims.Domain != "" {
						// TODO:
						// domain, err = s.domain.SayHello(ctx, claims.Domain)
						// if err != nil {
						// 	span.RecordError(err)
						// 	goto skip
						// }
					} else {
						span.RecordError(err)
						goto skip
					}
				}

				tags := core.ParseTags(domain.Tag)
				if tags.Has("_block") {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are blocked",
					})
				}

				ccid := claims.Principal
				// pull entity from remote if not registered
				_, err = s.entity.GetAddress(ctx, ccid)
				if err != nil {
					_, err = s.entity.PullEntityFromRemote(ctx, ccid, domain.ID)
					if err != nil {
						span.RecordError(err)
						goto skip
					}
				}

				c.Set(core.RequesterTypeCtxKey, core.RemoteUser)
				c.Set(core.RequesterIdCtxKey, ccid)
				c.Set(core.RequesterDomainCtxKey, domain.ID)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.RemoteUser)))
				span.SetAttributes(attribute.String("RequesterId", ccid))
				span.SetAttributes(attribute.String("RequesterDomain", domain.ID))
			}
		}
	skip:
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func ReceiveGatewayAuthPropagation(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, span := tracer.Start(c.Request().Context(), "auth.ReceiveGatewayAuthPropagation")
		defer span.End()

		reqTypeHeader := c.Request().Header.Get(core.RequesterTypeHeader)
		reqIdHeader := c.Request().Header.Get(core.RequesterIdHeader)
		reqTagHeader := c.Request().Header.Get(core.RequesterTagHeader)
		reqDomainHeader := c.Request().Header.Get(core.RequesterDomainHeader)
		reqKeyDepathHeader := c.Request().Header.Get(core.RequesterKeyDepathHeader)
		reqDomainTagsHeader := c.Request().Header.Get(core.RequesterDomainTagsHeader)
		reqRemoteTagsHeader := c.Request().Header.Get(core.RequesterRemoteTagsHeader)

		if reqTypeHeader != "" {
			reqType, err := strconv.Atoi(reqTypeHeader)
			if err == nil {
				c.Set(core.RequesterTypeCtxKey, reqType)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(reqType)))
			}
		}

		if reqIdHeader != "" {
			c.Set(core.RequesterIdCtxKey, reqIdHeader)
			span.SetAttributes(attribute.String("RequesterId", reqIdHeader))
		}

		if reqTagHeader != "" {
			c.Set(core.RequesterTagCtxKey, core.ParseTags(reqTagHeader))
			span.SetAttributes(attribute.String("RequesterTag", reqTagHeader))
		}

		if reqDomainHeader != "" {
			c.Set(core.RequesterDomainCtxKey, reqDomainHeader)
			span.SetAttributes(attribute.String("RequesterDomain", reqDomainHeader))
		}

		if reqKeyDepathHeader != "" {
			c.Set(core.RequesterKeyDepathKey, reqKeyDepathHeader)
			span.SetAttributes(attribute.String("RequesterKeyDepath", reqKeyDepathHeader))
		}

		if reqDomainTagsHeader != "" {
			c.Set(core.RequesterDomainTagsKey, core.ParseTags(reqDomainTagsHeader))
			span.SetAttributes(attribute.String("RequesterDomainTags", reqDomainTagsHeader))
		}

		if reqRemoteTagsHeader != "" {
			c.Set(core.RequesterRemoteTagsKey, core.ParseTags(reqRemoteTagsHeader))
			span.SetAttributes(attribute.String("RequesterRemoteTags", reqRemoteTagsHeader))
		}

		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func Restrict(principal Principal) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, span := tracer.Start(c.Request().Context(), "auth.Restrict")
			defer span.End()

			requesterType, _ := c.Get(core.RequesterTypeCtxKey).(int)
			requesterTags, _ := c.Get(core.RequesterTagCtxKey).(core.Tags)

			switch principal {
			case ISADMIN:
				if !requesterTags.Has("_admin") {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not admin",
					})
				}

			case ISLOCAL:
				if requesterType != core.LocalUser {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not local",
					})
				}

			case ISKNOWN:
				if requesterType == core.Unknown {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not known",
					})
				}

			case ISUNITED:
				if requesterType != core.RemoteDomain {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not united",
					})
				}
			}

			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}
