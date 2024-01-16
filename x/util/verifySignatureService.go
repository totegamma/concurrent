// Package util provides various utility functions
package util

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"golang.org/x/crypto/sha3"
	"strconv"
	"strings"
	"time"
)

// VerifySignature verifies a keccak256 signature
func VerifySignature(message string, address string, signature string) error {

	// R値とS値をbig.Intに変換
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return err
	}

	// メッセージをKeccak256でハッシュ化
	hash := sha3.NewLegacyKeccak256()
	hash.Write([]byte(message))
	hashedMessage := hash.Sum(nil)

	recoveredPub, err := crypto.Ecrecover(hashedMessage, sigBytes)
	if err != nil {
		return err
	}

	pubkey, err := crypto.UnmarshalPubkey(recoveredPub)
	if err != nil {
		return err
	}
	sigaddr := crypto.PubkeyToAddress(*pubkey)

	verified := address[2:] == sigaddr.Hex()[2:]
	if verified {
		return nil
	}

	return errors.New("signature validation failed")
}

// VerifySignatureFromBytes verifies a keccak256 signature
func VerifySignatureFromBytes(message []byte, signature []byte, address string) error {

	// メッセージをKeccak256でハッシュ化
	hash := sha3.NewLegacyKeccak256()
	hash.Write(message)
	hashedMessage := hash.Sum(nil)

	recoveredPub, err := crypto.Ecrecover(hashedMessage, signature)
	if err != nil {
		return err
	}

	pubkey, err := crypto.UnmarshalPubkey(recoveredPub)
	if err != nil {
		return err
	}
	sigaddr := crypto.PubkeyToAddress(*pubkey)

	verified := address[2:] == sigaddr.Hex()[2:]
	if verified {
		return nil
	}

	return errors.New("signature validation failed")
}

// CreateJWT creates server signed JWT
func CreateJWT(claims JwtClaims, privatekey string) (string, error) {
	header := JwtHeader{
		Type:      "JWT",
		Algorithm: "ECRECOVER",
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
	hash := sha3.NewLegacyKeccak256()
	hash.Write([]byte(target))
	hashedMessage := hash.Sum(nil)

	serverkey, err := crypto.HexToECDSA(privatekey)
	if err != nil {
		return "", err
	}
	signatureBytes, err := crypto.Sign([]byte(hashedMessage), serverkey)
	if err != nil {
		return "", err
	}

	signatureB64 := base64.RawURLEncoding.EncodeToString(signatureBytes)

	return target + "." + signatureB64, nil

}

func SignBytes(bytes []byte, privatekey string) (string, error) {

	hash := sha3.NewLegacyKeccak256()
	hash.Write(bytes)
	hashedMessage := hash.Sum(nil)

	serverkey, err := crypto.HexToECDSA(privatekey)
	if err != nil {
		return "", err
	}
	signatureBytes, err := crypto.Sign([]byte(hashedMessage), serverkey)
	if err != nil {
		return "", err
	}

	encoded := hex.EncodeToString(signatureBytes)

	return encoded, nil
}

// ValidateJWT checks is jwt signature valid and not expired
func ValidateJWT(jwt string) (JwtClaims, error) {

	var header JwtHeader
	var claims JwtClaims

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

	err = VerifySignatureFromBytes([]byte(split[0]+"."+split[1]), signatureBytes, claims.Issuer)
	if err != nil {
		return claims, err
	}

	// all checks passed
	return claims, nil
}

// JwtHeader is jwt header type
type JwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

// JwtClaims is jwt payload type
type JwtClaims struct {
    Issuer         string `json:"iss,omitempty"` // 発行者
    Subject        string `json:"sub,omitempty"` // 用途
    Audience       string `json:"aud,omitempty"` // 想定利用者
    ExpirationTime string `json:"exp,omitempty"` // 失効時刻
    IssuedAt       string `json:"iat,omitempty"` // 発行時刻
    JWTID          string `json:"jti,omitempty"` // JWT ID
    Tag            string `json:"tag,omitempty"` // comma separated list of tags
    Scope          string `json:"scp,omitempty"` // semicomma separated list of scopes
    Principal      string `json:"prn,omitempty"` // principal
}
