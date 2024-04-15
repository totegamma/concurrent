package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/util"
	"github.com/xinguang/go-recaptcha"
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
		passportHeader := c.Request().Header.Get("passport")

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

			if claims.Subject != "CC_API" {
				span.RecordError(fmt.Errorf("invalid subject"))
				goto skip
			}

			ccid := ""
			if passportHeader != "" { // treat as remote user
				// validate
				var passport core.Passport
				err = json.Unmarshal([]byte(passportHeader), &passport)
				if err != nil {
					span.RecordError(err)
					goto skip
				}

				var passportDoc core.PassportDocument
				err = json.Unmarshal([]byte(passport.Document), &passportDoc)
				if err != nil {
					span.RecordError(err)
					goto skip
				}

				if passportDoc.Domain == s.config.Concurrent.FQDN {
					span.RecordError(fmt.Errorf("do not use passport for local user"))
					goto skip
				}

				domain, err := s.domain.GetByFQDN(ctx, passportDoc.Domain)
				if err != nil {
					span.RecordError(err)
					goto skip
				}

				domainTags := core.ParseTags(domain.Tag)
				if domainTags.Has("_block") {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "your domain is blocked",
					})
				}

				err = util.VerifySignature([]byte(passport.Document), []byte(passport.Signature), domain.CCID)
				if err != nil { // TODO: this is misbehaving. should be logged to audit
					span.RecordError(err)
					goto skip
				}

				resolved, err := key.ValidateKeyResolution(passportDoc.Keys)
				if err != nil {
					span.RecordError(err)
					goto skip
				}

				if resolved != passportDoc.Entity.ID {
					span.RecordError(fmt.Errorf("Signer is not matched with the resolved signer"))
					goto skip
				}

				entity := passportDoc.Entity
				entityTags := core.ParseTags(entity.Tag)
				updated, err := s.entity.Affiliation(ctx, entity.AffiliationDocument, entity.AffiliationSignature, "")
				if err != nil {
					span.RecordError(err)
					return c.JSON(http.StatusForbidden, echo.Map{
						"error": "your affiliation document is invalid",
					})
				}

				if !updated.IsScoreFixed && updated.Score != entity.Score {
					entity.Score = updated.Score
					s.entity.UpdateScore(ctx, entity.ID, entity.Score)
				}

				ccid = passportDoc.Entity.ID

				c.Set(core.RequesterTypeCtxKey, core.RemoteUser)
				c.Set(core.RequesterTagCtxKey, entityTags)
				c.Set(core.RequesterDomainTagsKey, domainTags)
				c.Set(core.RequesterKeychainKey, passportDoc.Keys)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.RemoteUser)))
				span.SetAttributes(attribute.String("RequesterTag", entity.Tag))

			} else { // treat as local user
				if core.IsCCID(claims.Issuer) {
					ccid = claims.Issuer
				} else if core.IsCKID(claims.Issuer) {
					ccid, err = s.key.ResolveSubkey(ctx, claims.Issuer)
					if err != nil {
						span.RecordError(err)
						goto skip
					}
				} else {
					span.RecordError(fmt.Errorf("invalid issuer"))
					goto skip
				}

				entity, err := s.entity.Get(ctx, ccid)
				if err != nil {
					span.RecordError(err)
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not registered",
					})
				}

				tags := core.ParseTags(entity.Tag)
				c.Set(core.RequesterTypeCtxKey, core.LocalUser)
				c.Set(core.RequesterTagCtxKey, tags)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.LocalUser)))
				span.SetAttributes(attribute.String("RequesterTag", entity.Tag))
			}

			c.Set(core.RequesterIdCtxKey, ccid)
			span.SetAttributes(attribute.String("RequesterId", ccid))
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
		reqKeyHeader := c.Request().Header.Get(core.RequesterKeychainHeader)
		reqDomainTagsHeader := c.Request().Header.Get(core.RequesterDomainTagsHeader)
		reqCaptchaVerifiedHeader := c.Request().Header.Get(core.CaptchaVerifiedHeader)

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

		if reqKeyHeader != "" {
			var keys []core.Key
			err := json.Unmarshal([]byte(reqKeyHeader), &keys)
			if err == nil {
				c.Set(core.RequesterKeychainKey, keys)
				span.SetAttributes(attribute.String("RequesterKeychain", reqKeyHeader))
			}
		}

		if reqDomainTagsHeader != "" {
			c.Set(core.RequesterDomainTagsKey, core.ParseTags(reqDomainTagsHeader))
			span.SetAttributes(attribute.String("RequesterDomainTags", reqDomainTagsHeader))
		}

		if reqCaptchaVerifiedHeader != "" {
			c.Set(core.CaptchaVerifiedKey, true)
			span.SetAttributes(attribute.String("CaptchaVerified", reqCaptchaVerifiedHeader))
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

func Recaptcha(validator *recaptcha.ReCAPTCHA) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, span := tracer.Start(c.Request().Context(), "middleware.Recaptcha")
			defer span.End()

			challenge := c.Request().Header.Get("captcha")
			if challenge != "" {
				err := validator.Verify(challenge)
				if err == nil {
					span.AddEvent("captcha verified")
					c.Set(core.CaptchaVerifiedKey, true)
				} else {
					span.AddEvent("captcha verification failed")
				}
			} else {
				span.AddEvent("captcha challenge not found")
			}

			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}
