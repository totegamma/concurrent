package auth

import (
    "encoding/base64"
    "encoding/json"
    "fmt"
    "strings"

    // "github.com/totegamma/concurrent/x/core"
    "github.com/totegamma/concurrent/x/util"
)

// Service is entity service
type Service struct {
    config util.Config
}

// NewService is for wire.go
func NewService(config util.Config) *Service {
    return &Service{}
}

func ValidateJWT(author string) error {
    var jwt string = ""
    split := strings.Split(jwt, ".")
    if (len(split) != 3) {
        return fmt.Errorf("invalid jwt format")
    }

    var headerBytes []byte
    var payloadBytes []byte
    var signatureBytes []byte

    _, err := base64.RawURLEncoding.Decode(headerBytes, []byte(split[0]))
    if err != nil {
        return err
    }

    var header jwtheader
    err = json.Unmarshal(headerBytes, &header)
    if err != nil {
        return err
    }
    if header.Type != "JWT" || header.Algorithm != "ECRECOVER" {
        return fmt.Errorf("Unsupported JWT type")
    }
    _, err = base64.RawURLEncoding.Decode(payloadBytes, []byte(split[1]))
    if err != nil {
        return err
    }
    var claims jwtclaims
    err = json.Unmarshal(payloadBytes, &claims)
    if err != nil {
        return err
    }
    // exp check
    _, err = base64.RawURLEncoding.Decode(signatureBytes, []byte(split[2]))
    if err != nil {
        return err
    }
    // signature check (try to ecrecover)
    err = util.VerifySignatureFromBytes([]byte(split[0] + "." + split[1]), signatureBytes, claims.Issuer)
    if err != nil {
        return err
    }
    // iss check
    if author != claims.Issuer {
        return fmt.Errorf("Issuer not match")
    }

    return nil
}

type jwtheader struct {
    Algorithm string `json:"alg"`
    Type string `json:"typ"`
}

type jwtclaims struct {
    IssuedAt string `json:"iat"`
    Issuer string `json:"iss"`
}



