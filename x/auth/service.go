package auth

import (
    "fmt"
    "time"
    "context"
    "strconv"
    "github.com/rs/xid"
    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/entity"
)

// Service is entity service
type Service struct {
    config util.Config
    entity *entity.Service
}

// NewService is for wire.go
func NewService(config util.Config, entity *entity.Service) *Service {
    return &Service{config, entity}
}


// IssueJWT takes client signed JWT and returns server signed JWT
func (s *Service) IssueJWT(ctx context.Context, request string) (string, error) {
    ctx, childSpan := tracer.Start(ctx, "ServiceIssueJWT")
    defer childSpan.End()

    // check jwt basic info
    claims, err := util.ValidateJWT(request)
    if err != nil {
        return "", err
    }

    // TODO: check jti not used recently

    // check aud
    if claims.Audience != s.config.Concurrent.FQDN {
        return "", fmt.Errorf("jwt is not for this host")
    }

    // check if issuer exists in this host
    _, err = s.entity.Get(ctx, claims.Issuer)
    if err != nil {
        return "", err
    }

    // create new jwt
    response, err := util.CreateJWT(util.JwtClaims {
        Issuer: s.config.Concurrent.CCAddr,
        Subject: "concurrent",
        Audience: claims.Issuer,
        ExpirationTime: strconv.FormatInt(time.Now().Add(6 * time.Hour).Unix(), 10),
        NotBefore: strconv.FormatInt(time.Now().Unix(), 10),
        IssuedAt: strconv.FormatInt(time.Now().Unix(), 10),
        JWTID: xid.New().String(),
    }, s.config.Concurrent.Prvkey)

    if err != nil {
        return "", err
    }

    // TODO: register jti

    return response, nil
}

