package auth

import (
    "fmt"
    "time"
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
func (s *Service) IssueJWT(request string) (string, error) {
    // check jwt basic info
    claims, err := util.ValidateJWT(request)
    if err != nil {
        return "", err
    }

    // TODO: check jti not used recently

    // check aud
    if claims.Audience != s.config.FQDN {
        return "", fmt.Errorf("jwt is not for this host")
    }

    // check if issuer exists in this host
    _, err = s.entity.Get(claims.Issuer)
    if err != nil {
        return "", err
    }

    // create new jwt
    response, err := util.CreateJWT(util.JwtClaims {
        Issuer: s.config.CCAddr,
        Subject: "concurrent",
        Audience: claims.Issuer,
        ExpirationTime: strconv.FormatInt(time.Now().Add(6 * time.Hour).Unix(), 10),
        NotBefore: strconv.FormatInt(time.Now().Unix(), 10),
        IssuedAt: strconv.FormatInt(time.Now().Unix(), 10),
        JWTID: xid.New().String(),
    }, s.config.Prvkey)

    if err != nil {
        return "", err
    }

    // TODO: register jti

    return response, nil
}

