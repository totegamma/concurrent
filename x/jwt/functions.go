package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/totegamma/concurrent/core"
)

// Create creates server signed JWT
func Create(claims Claims, privatekey string) (string, error) {
	header := Header{
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
func Validate(jwt string) (Claims, error) {

	var header Header
	var claims Claims

	split := strings.Split(jwt, ".")
	if len(split) != 3 {
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
	if header.Type != "JWT" || header.Algorithm != "CONCRNT" {
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

	err = core.VerifySignature([]byte(split[0]+"."+split[1]), signatureBytes, claims.Issuer)
	if err != nil {
		return claims, err
	}

	// all checks passed
	return claims, nil
}
