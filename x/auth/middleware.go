package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key"
	"github.com/xinguang/go-recaptcha"
	"go.opentelemetry.io/otel/attribute"
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

			if passportDoc.Domain == s.config.FQDN {
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

			if core.IsCSID(passportDoc.Signer) && domain.CSID == "" {
				span.AddEvent("force fetch domain")
				_, err := s.domain.ForceFetch(ctx, domain.ID)
				if err != nil {
					span.RecordError(errors.Wrap(err, "failed to force fetch domain"))
					goto skipCheckPassport
				}
			}

			err = core.VerifySignature([]byte(passport.Document), signatureBytes, passportDoc.Signer)
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

			entity, err := s.entity.Get(ctx, ccid)
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to get entity"))
				return c.JSON(http.StatusForbidden, echo.Map{})
			}

			tags := core.ParseTags(entity.Tag)
			ctx = context.WithValue(ctx, core.RequesterTagCtxKey, tags)

			if tags.Has("_block") {
				return c.JSON(http.StatusForbidden, echo.Map{
					"error": "you are blocked",
				})
			}

			var domain core.Domain
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

				ctx = context.WithValue(ctx, core.RequesterIdCtxKey, ccid)
				span.SetAttributes(attribute.String("RequesterId", ccid))
				ctx = context.WithValue(ctx, core.RequesterTypeCtxKey, core.LocalUser)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.LocalUser)))
			} else {

				domain, err = s.domain.GetByFQDN(ctx, entity.Domain)
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
				ctx = context.WithValue(ctx, core.RequesterIdCtxKey, ccid)
				span.SetAttributes(attribute.String("RequesterId", ccid))
				ctx = context.WithValue(ctx, core.RequesterTypeCtxKey, core.RemoteUser)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(core.RemoteUser)))
				ctx = context.WithValue(ctx, core.RequesterDomainCtxKey, entity.Domain)
				span.SetAttributes(attribute.String("RequesterDomain", entity.Domain))
				ctx = context.WithValue(ctx, core.RequesterDomainTagsKey, domainTags)
				span.SetAttributes(attribute.String("RequesterDomainTags", domain.Tag))
			}

			_, err = s.entity.GetMeta(ctx, ccid)
			if err == nil {
				ctx = context.WithValue(ctx, core.RequesterIsRegisteredKey, true)
			} else {
				ctx = context.WithValue(ctx, core.RequesterIsRegisteredKey, false)
			}

			rctx := core.RequestContext{
				Requester:       entity,
				RequesterDomain: domain,
			}

			accessOK, err := s.policy.TestWithGlobalPolicy(ctx, rctx, "global")
			if err != nil {
				span.RecordError(errors.Wrap(err, "failed to test with global policy"))
				return c.JSON(http.StatusForbidden, echo.Map{})
			}

			if accessOK == core.PolicyEvalResultNever || accessOK == core.PolicyEvalResultDeny {
				return c.JSON(http.StatusForbidden, echo.Map{
					"error":  "you are not authorized to perform this action",
					"detail": "you are not allowed by global policy",
				})
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
		reqPassportHeader := c.Request().Header.Get(core.RequesterPassportHeader)
		reqRegisteredHeader := c.Request().Header.Get(core.RequesterIsRegisteredHeader)

		if reqTypeHeader != "" {
			reqType, err := strconv.Atoi(reqTypeHeader)
			if err == nil {
				ctx = context.WithValue(ctx, core.RequesterTypeCtxKey, reqType)
				span.SetAttributes(attribute.String("RequesterType", core.RequesterTypeString(reqType)))
			}
		}

		if reqIdHeader != "" {
			ctx = context.WithValue(ctx, core.RequesterIdCtxKey, reqIdHeader)
			span.SetAttributes(attribute.String("RequesterId", reqIdHeader))
		}

		if reqTagHeader != "" {
			ctx = context.WithValue(ctx, core.RequesterTagCtxKey, core.ParseTags(reqTagHeader))
			span.SetAttributes(attribute.String("RequesterTag", reqTagHeader))
		}

		if reqDomainHeader != "" {
			ctx = context.WithValue(ctx, core.RequesterDomainCtxKey, reqDomainHeader)
			span.SetAttributes(attribute.String("RequesterDomain", reqDomainHeader))
		}

		if reqKeyHeader != "" {
			var keys []core.Key
			err := json.Unmarshal([]byte(reqKeyHeader), &keys)
			if err == nil {
				ctx = context.WithValue(ctx, core.RequesterKeychainKey, keys)
				span.SetAttributes(attribute.String("RequesterKeychain", reqKeyHeader))
			}
		}

		if reqDomainTagsHeader != "" {
			ctx = context.WithValue(ctx, core.RequesterDomainTagsKey, core.ParseTags(reqDomainTagsHeader))
			span.SetAttributes(attribute.String("RequesterDomainTags", reqDomainTagsHeader))
		}

		if reqCaptchaVerifiedHeader != "" {
			ctx = context.WithValue(ctx, core.CaptchaVerifiedKey, true)
			span.SetAttributes(attribute.String("CaptchaVerified", reqCaptchaVerifiedHeader))
		}

		if reqPassportHeader != "" {
			ctx = context.WithValue(ctx, core.RequesterPassportKey, reqPassportHeader)
			span.SetAttributes(attribute.String("RequesterPassport", reqPassportHeader))
		}

		if reqRegisteredHeader != "" {
			registered, err := strconv.ParseBool(reqRegisteredHeader)
			if err == nil {
				ctx = context.WithValue(ctx, core.RequesterIsRegisteredKey, registered)
				span.SetAttributes(attribute.String("RequesterIsRegistered", reqRegisteredHeader))
			}
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

			requesterType, _ := ctx.Value(core.RequesterTypeCtxKey).(int)
			requesterTags, _ := ctx.Value(core.RequesterTagCtxKey).(core.Tags)

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
			case ISREGISTERED:
				if registered, _ := ctx.Value(core.RequesterIsRegisteredKey).(bool); !registered {
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

func (s *service) RateLimiter(configMap core.RateLimitConfigMap) echo.MiddlewareFunc {

	routerEcho := echo.New()
	dummyFunc := func(c echo.Context) error {
		return nil
	}

	if configMap == nil {
		configMap = core.RateLimitConfigMap{}
	}

	if _, ok := configMap["DEFAULT"]; !ok {
		configMap["DEFAULT"] = core.RateLimitConfig{
			BucketSize: 1000,
			RefillSpan: 1,
		}
	}

	core.JsonPrint("RateLimitConfigMap", configMap)

	for path := range configMap {
		if path == "DEFAULT" {
			continue
		}

		splitter := strings.Index(path, ":")
		if splitter == -1 {
			routerEcho.Any(path, dummyFunc)
			continue
		}

		method := path[:splitter]
		path = path[splitter+1:]

		switch method {
		case "GET":
			routerEcho.GET(path, dummyFunc)
		case "POST":
			routerEcho.POST(path, dummyFunc)
		case "PUT":
			routerEcho.PUT(path, dummyFunc)
		case "DELETE":
			routerEcho.DELETE(path, dummyFunc)
		case "PATCH":
			routerEcho.PATCH(path, dummyFunc)
		case "OPTIONS":
			routerEcho.OPTIONS(path, dummyFunc)
		case "HEAD":
			routerEcho.HEAD(path, dummyFunc)
		case "CONNECT":
			routerEcho.CONNECT(path, dummyFunc)
		case "TRACE":
			routerEcho.TRACE(path, dummyFunc)
		default:
			fmt.Println("Invalid method")
		}
	}

	resolvePath := func(c echo.Context) string {
		req := http.Request{}
		ctx := routerEcho.NewContext(&req, nil)
		routerEcho.Router().Find(c.Request().Method, c.Request().URL.Path, ctx)
		return fmt.Sprintf("%s:%s", c.Request().Method, ctx.Path())
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {

			ctx := c.Request().Context()
			path := resolvePath(c)

			// Skip rate limiting for OPTIONS requests
			if c.Request().Method == "OPTIONS" {
				return next(c)
			}

			config, ok := configMap[path]
			if !ok {
				config = configMap["DEFAULT"]
				path = "DEFAULT"
			}

			requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
			if !ok {
				requester = c.RealIP()
			}

			key := "rate_limit:" + requester + ":" + path

			// Get the current value of the bucket
			val, err := s.rdb.Get(ctx, key).Result()
			if err != nil {
				// If the key does not exist, create it
				value := fmt.Sprintf("%d;%d", time.Now().UnixMilli(), config.BucketSize)
				if err == redis.Nil {
					s.rdb.Set(
						ctx,
						key,
						value,
						time.Second*time.Duration(config.BucketSize)*time.Duration(config.RefillSpan),
					)

					c.Response().Header().Set("X-RateLimit-Limit", strconv.Itoa(config.BucketSize))
					c.Response().Header().Set("X-RateLimit-Remaining", strconv.Itoa(config.BucketSize))

					// Continue processing the request
					return next(c)
				} else {
					return err
				}
			}

			// Parse the value of the bucket
			split := strings.Split(val, ";")
			timestamp, _ := strconv.ParseInt(split[0], 10, 64)
			lastRefill := time.UnixMilli(timestamp)

			bucketCount, _ := strconv.Atoi(split[1])

			// Calculate the number of requests that should have been refilled
			now := time.Now()
			elapsed := now.Sub(lastRefill)
			refillCount := int(elapsed.Seconds() / config.RefillSpan)
			consumedTime := float64(refillCount) * config.RefillSpan
			nextRefill := lastRefill.Add(time.Second * time.Duration(consumedTime))

			// Refill the bucket
			bucketCount += refillCount

			// If the bucket is full, clip it
			if bucketCount > config.BucketSize {
				bucketCount = config.BucketSize
			}

			// subtract one from the bucket
			bucketCount -= 1

			// If the bucket is empty, return a 429
			if bucketCount < 0 {
				return c.String(http.StatusTooManyRequests, "Rate limit exceeded\n")
			}

			// Update the bucket
			value := fmt.Sprintf("%d;%d", nextRefill.UnixMilli(), bucketCount)
			s.rdb.Set(
				ctx,
				key,
				value,
				time.Second*time.Duration(config.BucketSize)*time.Duration(config.RefillSpan),
			)

			c.Response().Header().Set("X-RateLimit-Limit", strconv.Itoa(config.BucketSize))
			c.Response().Header().Set("X-RateLimit-Remaining", strconv.Itoa(bucketCount))

			// Continue processing the request
			return next(c)
		}
	}
}
