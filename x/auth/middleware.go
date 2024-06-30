package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"github.com/xinguang/go-recaptcha"
	"go.opentelemetry.io/otel/attribute"

	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key"
)

type Principal int

const (
	ISADMIN = iota
	ISLOCAL
	ISKNOWN
	ISUNITED
	ISREGISTERED
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

		if passportHeader != "" {
			ctx = context.WithValue(ctx, core.RequesterPassportKey, passportHeader)

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
			err = core.VerifySignature([]byte(passport.Document), signatureBytes, domain.CCID)
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
			updated, err := s.entity.Affiliation(ctx, core.CommitModeExecute, entity.AffiliationDocument, entity.AffiliationSignature, "")
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

			if updated.Alias != entity.Alias {
				_, err := s.entity.GetByAlias(ctx, *entity.Alias)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to get entity by alias"))
				}
			}

			ctx = context.WithValue(ctx, core.RequesterKeychainKey, passportDoc.Keys)
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

			if claims.Audience != s.config.FQDN {
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
				if providedKeyChain, ok := ctx.Value(core.RequesterKeychainKey).([]core.Key); ok {
					ccid, err = key.ValidateKeyResolution(providedKeyChain)
					if err != nil {
						span.RecordError(errors.Wrap(err, "failed to validate key resolution"))
						goto skipCheckAuthorization
					}
				} else {

					keys, err := s.key.GetKeyResolution(ctx, claims.Issuer)
					if err != nil {
						span.RecordError(errors.Wrap(err, "failed to get key resolution"))
						goto skipCheckAuthorization
					}
					ctx = context.WithValue(ctx, core.RequesterKeychainKey, keys)

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
			span.SetAttributes(attribute.String("RequesterId", ccid))

			entity, err := s.entity.Get(ctx, ccid)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to get entity"))
				return c.JSON(http.StatusForbidden, echo.Map{})
			}

			_, err = s.entity.GetMeta(ctx, ccid)
			registered := true
			if err != nil {
				if errors.Is(err, core.ErrorNotFound{}) {
					registered = false
				} else {
					span.RecordError(errors.Wrap(err, "failed to get entity meta"))
					return c.JSON(http.StatusForbidden, echo.Map{})
				}
			}

			if entity.Domain == s.config.FQDN {
				// local user

				if passportHeader == "" {
					keys, ok := ctx.Value(core.RequesterKeychainKey).([]core.Key)
					if !ok {
						keys = nil
					}
					passport, err := s.IssuePassport(ctx, ccid, keys)
					if err == nil {
						ctx = context.WithValue(ctx, core.RequesterPassportKey, passport)

					} else {
						span.RecordError(errors.Wrap(err, "failed to issue passport"))
					}
				}

				requesterContext := core.RequesterContext{
					Entity:       &entity,
					IsRegistered: registered,
				}

				ctx = context.WithValue(ctx, core.RequesterContextCtxKey, requesterContext)

				ctx = context.WithValue(ctx, core.RequesterTypeCtxKey, core.LocalUser)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.LocalUser)))
			} else {

				domain, err := s.domain.GetByFQDN(ctx, entity.Domain)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to get domain by fqdn"))
					return c.JSON(http.StatusForbidden, echo.Map{})
				}

				requesterContext := core.RequesterContext{
					Entity:       &entity,
					Domain:       &domain,
					IsRegistered: registered,
				}

				ctx = context.WithValue(ctx, core.RequesterContextCtxKey, requesterContext)

				ctx = context.WithValue(ctx, core.RequesterTypeCtxKey, core.RemoteUser)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.RemoteUser)))
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
		reqKeyHeader := c.Request().Header.Get(core.RequesterKeychainHeader)
		reqCaptchaVerifiedHeader := c.Request().Header.Get(core.CaptchaVerifiedHeader)
		reqPassportHeader := c.Request().Header.Get(core.RequesterPassportHeader)
		reqContextHeader := c.Request().Header.Get(core.RequesterContextHeader)

		if reqTypeHeader != "" {
			reqType, err := strconv.Atoi(reqTypeHeader)
			if err == nil {
				ctx = context.WithValue(ctx, core.RequesterTypeCtxKey, reqType)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(reqType)))
			}
		}

		if reqKeyHeader != "" {
			var keys []core.Key
			err := json.Unmarshal([]byte(reqKeyHeader), &keys)
			if err == nil {
				ctx = context.WithValue(ctx, core.RequesterKeychainKey, keys)
				span.SetAttributes(attribute.String("RequesterKeychain", reqKeyHeader))
			}
		}

		if reqCaptchaVerifiedHeader != "" {
			ctx = context.WithValue(ctx, core.CaptchaVerifiedKey, true)
			span.SetAttributes(attribute.String("CaptchaVerified", reqCaptchaVerifiedHeader))
		}

		if reqPassportHeader != "" {
			ctx = context.WithValue(ctx, core.RequesterPassportKey, reqPassportHeader)
			span.SetAttributes(attribute.String("RequesterPassport", reqPassportHeader))
		}

		if reqContextHeader != "" {
			var requesterContext core.RequesterContext
			err := json.Unmarshal([]byte(reqContextHeader), &requesterContext)
			if err == nil {
				ctx = context.WithValue(ctx, core.RequesterContextCtxKey, requesterContext)
				span.SetAttributes(attribute.String("RequesterContext", reqContextHeader))
			}
		}

		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func SetRequestPath(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, span := tracer.Start(c.Request().Context(), "Auth.Service.SetRequestPath")
		defer span.End()

		url := c.Request().URL
		method := c.Request().Method
		path := method + ":" + url.Path

		ctx = context.WithValue(ctx, core.RequestPathCtxKey, path)
		span.SetAttributes(attribute.String("RequestPath", path))

		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func Restrict(principal Principal) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, span := tracer.Start(c.Request().Context(), "Auth.Service.Restrict")
			defer span.End()

			requesterType, _ := ctx.Value(core.RequesterTypeCtxKey).(int)
			requesterContext, _ := ctx.Value(core.RequesterContextCtxKey).(core.RequesterContext)

			switch principal {
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

			case ISREGISTERED:
				if !requesterContext.IsRegistered {
					return c.JSON(http.StatusForbidden, echo.Map{
						"error":  "you are not authorized to perform this action",
						"detail": "you are not registered",
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
					ctx = context.WithValue(ctx, core.CaptchaVerifiedKey, true)
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
