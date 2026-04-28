package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"github.com/xinguang/go-recaptcha"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/client"
	"github.com/totegamma/concrnt-playground/impl/interop"
	"github.com/totegamma/concrnt-playground/internal/domain"
	"github.com/totegamma/concrnt-playground/internal/infra/repository"
	"github.com/totegamma/concrnt-playground/internal/usecase"
	"github.com/totegamma/concrnt-playground/jwt"
	"github.com/totegamma/concrnt-playground/schemas"
)

var tracer = otel.Tracer("auth")

type AuthMiddleware struct {
	config domain.Config
	client *client.Client
	server *usecase.ServerUsecase
	entity *repository.EntityRepository
}

func NewAuthMiddleware(
	config domain.Config,
	client *client.Client,
	server *usecase.ServerUsecase,
	entity *repository.EntityRepository,
) *AuthMiddleware {
	return &AuthMiddleware{
		config: config,
		client: client,
		server: server,
		entity: entity,
	}
}

func (s *AuthMiddleware) IdentifyIdentity(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, span := tracer.Start(c.Request().Context(), "Auth.Service.IdentifyIdentity")
		defer span.End()

		// # authtoken
		// 実体はjwtトークン
		// requesterが本人であることを証明するのに使う。
		authHeader := c.Request().Header.Get("authorization")

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

			header, claims, err := jwt.Parse(token)
			if err != nil {
				span.RecordError(errors.Wrap(err, "jwt validation failed"))
				goto skipCheckAuthorization
			}

			if claims.Audience != s.config.FQDN {
				err := fmt.Errorf("jwt audience mismatch: expected %s, got %s", s.config.FQDN, claims.Audience)
				span.RecordError(err)
				goto skipCheckAuthorization
			}

			if claims.Subject != "concrnt" {
				err := fmt.Errorf("invalid subject")
				span.RecordError(err)
				goto skipCheckAuthorization
			}

			issParsed, err := concrnt.ParseCCURI(claims.Issuer)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to parse issuer as CCURI"))
				goto skipCheckAuthorization
			}

			requester, err := s.entity.Get(ctx, issParsed.Owner, issParsed.Hint)
			if err != nil {
				span.RecordError(errors.Wrap(err, "AuthMiddleware.IdentifyIdentity: s.entity.Get failed"))
				goto skipCheckAuthorization
			}
			entityTag := requester.Tag()

			if entityTag.Has("_blocked") {
				err := fmt.Errorf("entity is blocked")
				return echo.NewHTTPError(403, err.Error())
			}

			srv, err := s.server.Resolve(ctx, requester.Domain, nil)
			if err != nil {
				span.RecordError(errors.Wrap(err, "AuthMiddleware.IdentifyIdentity: s.server.GetAndCacheByFQDN failed"))
				goto skipCheckAuthorization
			}
			srvTag := srv.Tag()

			if srvTag.Has("_blocked") {
				err := fmt.Errorf("server is blocked")
				return echo.NewHTTPError(403, err.Error())
			}

			keyID := header.KeyID
			if keyID == "" {
				keyID = claims.Issuer
			}

			parsed, err := concrnt.ParseCCURI(keyID)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to parse issuer as CCURI"))
				goto skipCheckAuthorization
			}
			ccid := parsed.Owner

			if parsed.Key == "" { // login as raw key

				err = jwt.Validate(token, ccid)
				if err != nil {
					span.RecordError(errors.Wrap(err, "jwt signature validation failed"))
					goto skipCheckAuthorization
				}

			} else { // login as subkey

				var subKeyDoc concrnt.Document[schemas.Subkey]
				// TODO 署名を確認するstrictオプションが必要
				// TODO キーのキャッシュが必須
				err := s.client.GetRecord(ctx, keyID, nil, &subKeyDoc)
				if err != nil {
					span.RecordError(err)
					goto skipCheckAuthorization
				}

				err = jwt.Validate(token, subKeyDoc.Value.CKID)
				if err != nil {
					span.RecordError(errors.Wrap(err, "jwt signature validation failed"))
					goto skipCheckAuthorization
				}
			}

			ctx = context.WithValue(ctx, interop.RequesterCtxKey, *requester)
			span.SetAttributes(attribute.String("RequesterId", ccid))

			ctx = context.WithValue(ctx, interop.RequesterTagCtxKey, entityTag)
			span.SetAttributes(attribute.String("RequesterTag", entityTag.ToString()))
		}

	skipCheckAuthorization:
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

// Recaptcha is a middleware factory that returns a middleware to verify reCAPTCHA challenges.
// It expects the challenge response in the "captcha" header and uses the provided validator.
// If verification is successful, it sets a flag in the context.
func Recaptcha(validator *recaptcha.ReCAPTCHA) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, span := tracer.Start(c.Request().Context(), "Auth.Service.Recaptcha")
			defer span.End()

			challenge := c.Request().Header.Get("captcha")
			if challenge != "" {
				err := validator.Verify(challenge)
				if err == nil {
					ctx = context.WithValue(ctx, interop.CaptchaVerifiedCtxKey, true)
				} else {
					ctx = context.WithValue(ctx, interop.CaptchaVerifiedCtxKey, false)
				}
			} else {
				ctx = context.WithValue(ctx, interop.CaptchaVerifiedCtxKey, false)
			}

			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}
