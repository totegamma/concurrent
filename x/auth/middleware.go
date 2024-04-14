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

				tags := core.ParseTags(domain.Tag)
				if tags.Has("_block") {
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

				// validate key
				err = key.ValidateKeyResolution(passportDoc.Keys, passportDoc.Entity.ID)
				if err != nil {
					span.RecordError(err)
					goto skip
				}

				entity := passportDoc.Entity
				oldentity, err := s.entity.Get(ctx, passportDoc.Entity.ID)
				if err == nil { // Already registered
					shouldUpdate := false
					tags = core.ParseTags(oldentity.Tag)
					if tags.Has("_block") {
						return c.JSON(http.StatusForbidden, echo.Map{
							"error":  "you are not authorized to perform this action",
							"detail": "you are blocked",
						})
					}
					if !oldentity.IsScoreFixed {
						if entity.Score != oldentity.Score {
							shouldUpdate = true
							entity.Score = oldentity.Score
						}
					}

					if entity.Domain != oldentity.Domain {
						shouldUpdate = true
						// validate affiliation
						err = util.VerifySignature([]byte(entity.AffiliationDocument), []byte(entity.AffiliationSignature), oldentity.ID)
						if err != nil {
							span.RecordError(err)
							goto skip
						}

						var affiliation core.EntityAffiliation
						err = json.Unmarshal([]byte(entity.AffiliationDocument), &affiliation)
						if err != nil {
							span.RecordError(err)
							return c.JSON(http.StatusForbidden, echo.Map{
								"error": "your affiliation document is invalid",
							})
						}

						var oldaffiliation core.EntityAffiliation
						err = json.Unmarshal([]byte(oldentity.AffiliationDocument), &oldaffiliation)
						if err != nil {
							span.RecordError(err)
							goto skip
						}

						if affiliation.SignedAt.Before(oldaffiliation.SignedAt) {
							span.RecordError(fmt.Errorf("affiliation is outdated"))
							return c.JSON(http.StatusForbidden, echo.Map{
								"error": "your affiliation document is outdated",
							})
						}
						entity.AffiliationDocument = oldentity.AffiliationDocument
						entity.AffiliationSignature = oldentity.AffiliationSignature
					}

					if shouldUpdate {
						s.entity.Update(ctx, &entity)
					}
				} else {
					err = util.VerifySignature([]byte(entity.AffiliationDocument), []byte(entity.AffiliationSignature), entity.ID)
					if err != nil {
						span.RecordError(err)
						return c.JSON(http.StatusForbidden, echo.Map{
							"error": "your affiliation document is invalid",
						})
					}
					if entity.Domain != passportDoc.Domain {
						span.RecordError(fmt.Errorf("invalid domain"))
						return c.JSON(http.StatusForbidden, echo.Map{
							"error": "your affiliation document and passport is not consistent",
						})
					}
					s.entity.Update(ctx, &entity)
				}

				ccid = passportDoc.Entity.ID

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
		reqKeyDepathHeader := c.Request().Header.Get(core.RequesterKeyDepathHeader)
		reqDomainTagsHeader := c.Request().Header.Get(core.RequesterDomainTagsHeader)
		reqRemoteTagsHeader := c.Request().Header.Get(core.RequesterRemoteTagsHeader)
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
