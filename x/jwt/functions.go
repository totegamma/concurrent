package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/concrnt/concrnt/core"
)

// Create creates server signed JWT
func Create(claims core.JwtClaims, privatekey string) (string, error) {
	header := core.JwtHeader{
		Type:      "JWT",
		Algorithm: "CONCRNT",
	}
	headerStr, err := json.Marshal(header)
	if err != nil {
		return "", err
	}

	payloadStr, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(headerStr))
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte(payloadStr))
	target := headerB64 + "." + payloadB64

	signatureBytes, err := core.SignBytes([]byte(target), privatekey)
	signatureB64 := base64.RawURLEncoding.EncodeToString(signatureBytes)

	return target + "." + signatureB64, nil

}

// Validate checks is jwt signature valid and not expired
func Validate(jwt string) (*core.JwtHeader, *core.JwtClaims, error) {

	split := strings.Split(jwt, ".")
	if len(split) != 3 {
		return nil, nil, fmt.Errorf("invalid jwt format")
	}

	var header core.JwtHeader
	headerBytes, err := base64.RawURLEncoding.DecodeString(split[0])
	if err != nil {
		return nil, nil, err
	}
	err = json.Unmarshal(headerBytes, &header)
	if err != nil {
		return nil, nil, err
	}

	// check jwt type
	if header.Type != "JWT" || header.Algorithm != "CONCRNT" {
		return nil, nil, fmt.Errorf("Unsupported JWT type")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(split[1])
	if err != nil {
		return nil, nil, err
	}

	var claims core.JwtClaims
	err = json.Unmarshal(payloadBytes, &claims)
	if err != nil {
		return nil, nil, err
	}

	// check exp
	if claims.ExpirationTime != "" {
		exp, err := strconv.ParseInt(claims.ExpirationTime, 10, 64)
		if err != nil {
			return nil, nil, err
		}
		now := time.Now().Unix()
		if exp < now {
			return nil, nil, fmt.Errorf("jwt is already expired")
		}
	}

	// check signature
	signatureBytes, err := base64.RawURLEncoding.DecodeString(split[2])
	if err != nil {
		return nil, nil, err
	}

	keyID := header.KeyID
	if keyID == "" {
		keyID = claims.Issuer
	}

	err = core.VerifySignature([]byte(split[0]+"."+split[1]), signatureBytes, keyID)
	if err != nil {
		return nil, nil, err
	}

	// all checks passed
	return &header, &claims, nil
}
