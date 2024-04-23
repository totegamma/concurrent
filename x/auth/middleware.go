package auth

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
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
		ctx, span := tracer.Start(c.Request().Context(), "Auth.Service.IdentifyIdentity")
		defer span.End()

		// # authtoken
		// 実体はjwtトークン
		// requesterが本人であることを証明するのに使う。
		authHeader := c.Request().Header.Get("authorization")
		// # passport
		// 実体はbase64エンコードされたjson
		// リクエストに必要な情報を補完するのに使う。
		passportHeader := c.Request().Header.Get("passport")

		span.SetAttributes(attribute.String("Authorization", authHeader))

		if passportHeader != "" {
			c.Set(core.RequesterPassportKey, passportHeader)

			passportJson, err := base64.URLEncoding.DecodeString(passportHeader)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to decode passport"))
				goto skipCheckPassport
			}

			var passport core.Passport
			err = json.Unmarshal(passportJson, &passport)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to unmarshal passport"))
				goto skipCheckPassport
			}

			var passportDoc core.PassportDocument
			err = json.Unmarshal([]byte(passport.Document), &passportDoc)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to unmarshal passport document"))
				goto skipCheckPassport
			}

			if passportDoc.Domain == s.config.Concurrent.FQDN {
				span.RecordError(fmt.Errorf("do not use passport for local user"))
				goto skipCheckPassport
			}

			domain, err := s.domain.GetByFQDN(ctx, passportDoc.Domain)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to get domain by fqdn"))
				goto skipCheckPassport
			}

			signatureBytes, err := hex.DecodeString(passport.Signature)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to decode signature"))
				goto skipCheckPassport
			}
			err = util.VerifySignature([]byte(passport.Document), signatureBytes, domain.CCID)
			if err != nil { // TODO: this is misbehaving. should be logged to audit
				span.RecordError(errors.Wrap(err, "failed to verify signature of passport"))
				goto skipCheckPassport
			}

			if len(passportDoc.Keys) > 0 {
				resolved, err := key.ValidateKeyResolution(passportDoc.Keys)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to validate key resolution"))
					goto skipCheckPassport
				}

				if resolved != passportDoc.Entity.ID {
					span.RecordError(fmt.Errorf("Signer is not matched with the resolved signer. expected: %s, actual: %s", resolved, passportDoc.Entity.ID))
					goto skipCheckPassport
				}
			}

			entity := passportDoc.Entity
			updated, err := s.entity.Affiliation(ctx, entity.AffiliationDocument, entity.AffiliationSignature, "")
			if err != nil {
				span.RecordError(err)
				return c.JSON(http.StatusForbidden, echo.Map{
					"error": "your affiliation document is invalid",
				})
			}

			if !updated.IsScoreFixed && updated.Score != entity.Score {
				err := s.entity.UpdateScore(ctx, entity.ID, entity.Score)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to update score"))
				}
			}

			c.Set(core.RequesterKeychainKey, passportDoc.Keys)
		}
	skipCheckPassport:

		if authHeader != "" {
			split := strings.Split(authHeader, " ")
			if len(split) != 2 {
				span.RecordError(fmt.Errorf("invalid authentication header"))
				goto skipCheckAuthorization
			}

			authType, token := split[0], split[1]
			if authType != "Bearer" {
				span.RecordError(fmt.Errorf("only Bearer is acceptable"))
				goto skipCheckAuthorization
			}

			claims, err := jwt.Validate(token)
			if err != nil {
				span.RecordError(errors.Wrap(err, "jwt validation failed"))
				goto skipCheckAuthorization
			}

			if claims.Audience != s.config.Concurrent.FQDN {
				span.RecordError(fmt.Errorf("jwt is not for this domain"))
				goto skipCheckAuthorization
			}

			if claims.Subject != "concrnt" {
				span.RecordError(fmt.Errorf("invalid subject"))
				goto skipCheckAuthorization
			}

			var ccid string
			if core.IsCCID(claims.Issuer) {
				ccid = claims.Issuer
			} else if core.IsCKID(claims.Issuer) {
				if providedKeyChain, ok := c.Get(core.RequesterKeychainKey).([]core.Key); ok {
					ccid, err = key.ValidateKeyResolution(providedKeyChain)
					if err != nil {
						span.RecordError(errors.Wrap(err, "failed to validate key resolution"))
						goto skipCheckAuthorization
					}
				} else {
					ccid, err = s.key.ResolveSubkey(ctx, claims.Issuer)
					if err != nil {
						span.RecordError(errors.Wrap(err, "failed to resolve subkey"))
						goto skipCheckAuthorization
					}
				}
			} else {
				span.RecordError(fmt.Errorf("invalid issuer"))
				goto skipCheckAuthorization
			}

			entity, err := s.entity.Get(ctx, ccid)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to get entity"))
				return c.JSON(http.StatusForbidden, echo.Map{})
			}

			tags := core.ParseTags(entity.Tag)
			c.Set(core.RequesterTagCtxKey, tags)

			if tags.Has("_block") {
				return c.JSON(http.StatusForbidden, echo.Map{
					"error": "you are blocked",
				})
			}

			if entity.Domain == s.config.Concurrent.FQDN {
				// local user
				c.Set(core.RequesterIdCtxKey, ccid)
				span.SetAttributes(attribute.String("RequesterId", ccid))
				c.Set(core.RequesterTypeCtxKey, core.LocalUser)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.LocalUser)))
			} else {

				domain, err := s.domain.GetByFQDN(ctx, entity.Domain)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to get domain by fqdn"))
					return c.JSON(http.StatusForbidden, echo.Map{})
				}

				domainTags := core.ParseTags(domain.Tag)
				if domainTags.Has("_block") {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "your domain is blocked",
					})
				}

				// remote user
				c.Set(core.RequesterIdCtxKey, ccid)
				span.SetAttributes(attribute.String("RequesterId", ccid))
				c.Set(core.RequesterTypeCtxKey, core.RemoteUser)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.RemoteUser)))
				c.Set(core.RequesterDomainCtxKey, entity.Domain)
				span.SetAttributes(attribute.String("RequesterDomain", entity.Domain))
				c.Set(core.RequesterDomainTagsKey, domainTags)
				span.SetAttributes(attribute.String("RequesterDomainTags", domain.Tag))
			}
		}
	skipCheckAuthorization:
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func ReceiveGatewayAuthPropagation(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, span := tracer.Start(c.Request().Context(), "Auth.Service.ReceiveGatewayAuthPropagation")
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
			ctx, span := tracer.Start(c.Request().Context(), "Auth.Service.Restrict")
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
			ctx, span := tracer.Start(c.Request().Context(), "Auth.Service.Recaptcha")
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
