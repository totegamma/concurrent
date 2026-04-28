package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/totegamma/concrnt-playground"
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

	signatureBytes, err := concrnt.SignBytes([]byte(target), privatekey)
	signatureB64 := base64.RawURLEncoding.EncodeToString(signatureBytes)

	return target + "." + signatureB64, nil

}

// Validate checks is jwt signature valid and not expired
func Validate(jwt string, expectedSigner string) error {

	header, claims, err := Parse(jwt)
	if err != nil {
		return err
	}

	// check jwt type
	if header.Type != "JWT" || header.Algorithm != "CONCRNT" {
		return fmt.Errorf("Unsupported JWT type")
	}

	// check exp
	if claims.ExpirationTime != "" {
		exp, err := strconv.ParseInt(claims.ExpirationTime, 10, 64)
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		if exp < now {
			return fmt.Errorf("jwt is already expired")
		}
	}

	split := strings.Split(jwt, ".")
	if len(split) != 3 {
		return fmt.Errorf("invalid jwt format")
	}

	// check signature
	signatureBytes, err := base64.RawURLEncoding.DecodeString(split[2])
	if err != nil {
		return err
	}

	err = concrnt.VerifySignature([]byte(split[0]+"."+split[1]), signatureBytes, expectedSigner)
	if err != nil {
		return err
	}

	// all checks passed
	return nil
}

func Parse(jwt string) (*Header, *Claims, error) {

	split := strings.Split(jwt, ".")
	if len(split) != 3 {
		return nil, nil, fmt.Errorf("invalid jwt format")
	}

	var header Header
	headerBytes, err := base64.RawURLEncoding.DecodeString(split[0])
	if err != nil {
		return nil, nil, err
	}
	err = json.Unmarshal(headerBytes, &header)
	if err != nil {
		return nil, nil, err
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(split[1])
	if err != nil {
		return nil, nil, err
	}

	var claims Claims
	err = json.Unmarshal(payloadBytes, &claims)
	if err != nil {
		return nil, nil, err
	}

	return &header, &claims, nil
}
