package auth

import (
    "fmt"
    "time"
    "strconv"
    "strings"
    "encoding/json"
    "encoding/base64"

    // "github.com/totegamma/concurrent/x/core"
    "github.com/totegamma/concurrent/x/util"
)

// Service is entity service
type Service struct {
    config util.Config
}

// NewService is for wire.go
func NewService(config util.Config) *Service {
    return &Service{config}
}

// ValidateJWT checks is jwt signature valid and not expired
func (s *Service) ValidateJWT(jwt string) (JwtClaims, error) {

    var header JwtHeader
    var claims JwtClaims

    split := strings.Split(jwt, ".")
    if (len(split) != 3) {
        return claims, fmt.Errorf("invalid jwt format")
    }

    headerBytes, err := base64.RawURLEncoding.DecodeString(split[0])
    if err != nil {
        return claims, err
    }
    err = json.Unmarshal(headerBytes, &header)
    if err != nil {
        return claims, err
    }
    
    // check jwt type
    if header.Type != "JWT" || header.Algorithm != "ECRECOVER" {
        return claims, fmt.Errorf("Unsupported JWT type")
    }

    payloadBytes, err := base64.RawURLEncoding.DecodeString(split[1])
    if err != nil {
        return claims, err
    }
    err = json.Unmarshal(payloadBytes, &claims)
    if err != nil {
        return claims, err
    }

    // check aud
    if claims.Audience != s.config.FQDN {
        return claims, fmt.Errorf("jwt is not for this host")
    }

    // check nbf
    if claims.NotBefore != "" {
        nbf, err := strconv.ParseInt(claims.NotBefore, 10, 64)
        if err != nil {
            return claims, err
        }
        now := time.Now().Unix()
        if now < nbf {
            return claims, fmt.Errorf("jwt is not valid yet")
        }
    }

    // check exp
    if claims.ExpirationTime != "" {
        exp, err := strconv.ParseInt(claims.ExpirationTime, 10, 64)
        if err != nil {
            return claims, err
        }
        now := time.Now().Unix()
        if exp < now {
            return claims, fmt.Errorf("jwt is already expired")
        }
    }

    // check signature
    signatureBytes, err := base64.RawURLEncoding.DecodeString(split[2])
    if err != nil {
        return claims, err
    }

    err = util.VerifySignatureFromBytes([]byte(split[0] + "." + split[1]), signatureBytes, claims.Issuer)
    if err != nil {
        return claims, err
    }

    // all checks passed
    return claims, nil
}


// JwtHeader is jwt header type
type JwtHeader struct {
    Algorithm string `json:"alg"`
    Type string `json:"typ"`
}

// JwtClaims is jwt payload type
type JwtClaims struct {
    Issuer string `json:"iss"` // 発行者
    Subject string `json:"sub"` // 用途
    Audience string `json:"aud"` // 想定利用者
    ExpirationTime string `json:"exp"` // 失効時刻
    NotBefore string `json:"nbf"` // 有効になる時刻
    IssuedAt string `json:"iat"` // 発行時刻
    JWTID string `json:"jti"` // JWT ID
}



