package core

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"gitlab.com/yawning/secp256k1-voi/secec"
	"golang.org/x/crypto/sha3"
	"strconv"
	"strings"
	"time"
)

func GetHash(bytes []byte) []byte {
	hash := sha3.NewLegacyKeccak256()
	hash.Write(bytes)
	return hash.Sum(nil)
}

func SignBytes(bytes []byte, privatekey string) ([]byte, error) {

	hash := sha3.NewLegacyKeccak256()
	hash.Write(bytes)
	hashed := hash.Sum(nil)

	key, err := crypto.HexToECDSA(privatekey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert private key")
	}

	signature, err := crypto.Sign(hashed, key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to sign message")
	}

	return signature, nil
}

func VerifySignature(message []byte, signature []byte, address string) error {

	hash := sha3.NewLegacyKeccak256()
	hash.Write(message)
	hashed := hash.Sum(nil)

	recoveredPub, err := crypto.Ecrecover(hashed, signature)
	if err != nil {
		return errors.Wrap(err, "failed to recover public key")
	}

	seckey, err := secec.NewPublicKey(recoveredPub)
	if err != nil {
		panic(err)
	}
	compressed := seckey.CompressedBytes()
	hrp := address[:3]
	sigaddr, err := PubkeyBytesToAddr(compressed, hrp)
	if err != nil {
		return errors.Wrap(err, "failed to convert public key to address")
	}

	if sigaddr != address {
		return errors.New("signature is not matched with address. expected: " + address + ", actual: " + sigaddr)
	}

	return nil
}

func PubkeyBytesToAddr(pubkeyBytes []byte, hrp string) (string, error) {
	pubkey := secp256k1.PubKey{
		Key: pubkeyBytes,
	}

	account := sdk.AccAddress(pubkey.Address())
	cdc := address.NewBech32Codec(hrp)
	addr, err := cdc.BytesToString(account)
	if err != nil {
		return "", errors.Wrap(err, "failed to convert address")
	}

	return addr, nil
}

func PubkeyToAddr(pubkeyHex string, hrp string) (string, error) {
	pubKeyBytes, err := hex.DecodeString(pubkeyHex)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode public key")
	}

	return PubkeyBytesToAddr(pubKeyBytes, hrp)
}

func PrivKeyToAddr(privKeyHex string, hrp string) (string, error) {
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode private key")
	}

	privKey := secp256k1.PrivKey{
		Key: privKeyBytes,
	}

	pubkey := privKey.PubKey()

	return PubkeyBytesToAddr(pubkey.Bytes(), hrp)
}

func SetupConfig(base ConfigInput) Config {

	ccid, err := PrivKeyToAddr(base.PrivateKey, "con")
	if err != nil {
		panic(err)
	}

	csid, err := PrivKeyToAddr(base.PrivateKey, "ccs")
	if err != nil {
		panic(err)
	}

	return Config{
		FQDN:         base.FQDN,
		PrivateKey:   base.PrivateKey,
		Registration: base.Registration,
		SiteKey:      base.SiteKey,
		Dimension:    base.Dimension,
		CCID:         ccid,
		CSID:         csid,
	}
}

func ValidateKeyResolution(keys []Key, startKey string) (string, error) {

	var rootKey string
	var nextKey string
	for _, key := range keys {

		if (nextKey == "") && (startKey != key.ID) {
			return "", fmt.Errorf("This key-resolution does not start with %s", startKey)
		}

		if (nextKey != "") && (nextKey != key.ID) {
			return "", fmt.Errorf("Key %s is not a child of %s", key.ID, nextKey)
		}

		signature, err := hex.DecodeString(key.EnactSignature)
		if err != nil {
			return "", err
		}
		err = VerifySignature([]byte(key.EnactDocument), signature, key.Parent)
		if err != nil {
			return "", err
		}

		var enact EnactDocument
		err = json.Unmarshal([]byte(key.EnactDocument), &enact)
		if err != nil {
			return "", err
		}

		if IsCCID(key.Parent) {
			if enact.Signer != key.Parent {
				return "", fmt.Errorf("enact signer is not matched with the parent")
			}
		} else {
			if enact.KeyID != key.Parent {
				return "", fmt.Errorf("enact keyID is not matched with the parent")
			}
		}

		if enact.Target != key.ID {
			return "", fmt.Errorf("KeyID in payload is not matched with the keyID")
		}

		if enact.Parent != key.Parent {
			return "", fmt.Errorf("Parent in payload is not matched with the parent")
		}

		if enact.Root != key.Root {
			return "", fmt.Errorf("Root in payload is not matched with the root")
		}

		if rootKey == "" {
			rootKey = key.Root
		} else {
			if rootKey != key.Root {
				return "", fmt.Errorf("Root is not matched with the previous key")
			}
		}

		if key.RevokeDocument != nil {
			return "", fmt.Errorf("Key %s is revoked", key.ID)
		}

		nextKey = key.Parent
	}

	return rootKey, nil
}

type VerifyJWTResult struct {
	Principal       string
	Audience        string
	Subject         string
	Domain          string
	DocumentSigner  string
	AffiliationDate time.Time
}

// note:
// Caller MUST check:
//  1. Audience is matched with the expected value (e.g. callback_url).
//  2. Subject is matched with the expected value (e.g. CONCRNT_3RD_PARTY_AUTH).
//  3. Retrive the Domain's csid and verify it matched the DocumentSigner.
//  4. For each challenge, the verifier MUST store the received affiliation date,
//     and any challenge containing an affiliation date older than the stored date MUST be rejected.
func VerifyJWT(jwtStr string, passportStr string) (*VerifyJWTResult, error) {

	var header JwtHeader
	var claims JwtClaims

	split := strings.Split(jwtStr, ".")
	if len(split) != 3 {
		return nil, fmt.Errorf("invalid jwt format")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(split[0])
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(headerBytes, &header)
	if err != nil {
		return nil, err
	}

	// check jwt type
	if header.Type != "JWT" || header.Algorithm != "CONCRNT" {
		return nil, fmt.Errorf("Unsupported JWT type")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(split[1])
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(payloadBytes, &claims)
	if err != nil {
		return nil, err
	}

	if claims.ExpirationTime != "" {
		exp, err := strconv.ParseInt(claims.ExpirationTime, 10, 64)
		if err != nil {
			return nil, err
		}
		now := time.Now().Unix()
		if exp < now {
			return nil, fmt.Errorf("jwt is already expired")
		}
	}

	// check signature
	signatureBytes, err := base64.RawURLEncoding.DecodeString(split[2])
	if err != nil {
		return nil, err
	}

	principal := claims.Issuer

	key := claims.Issuer
	if header.KeyID != "" {
		key = header.KeyID
	}

	err = VerifySignature([]byte(split[0]+"."+split[1]), signatureBytes, key)
	if err != nil {
		return nil, err
	}

	if IsCCID(key) {
		return &VerifyJWTResult{
			Principal: principal,
			Audience:  claims.Audience,
			Subject:   claims.Subject,
		}, nil
	}

	passportJson, err := base64.URLEncoding.DecodeString(passportStr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode passport")
	}

	var passport Passport
	err = json.Unmarshal(passportJson, &passport)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal passport")
	}

	var passportDoc PassportDocument
	err = json.Unmarshal([]byte(passport.Document), &passportDoc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal passport document")
	}

	signatureBytes, err = hex.DecodeString(passport.Signature)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode signature")
	}

	err = VerifySignature([]byte(passport.Document), signatureBytes, passportDoc.Signer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to verify signature")
	}

	resolved, err := ValidateKeyResolution(passportDoc.Keys, key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to validate key resolution")
	}

	if resolved != principal {
		return nil, errors.New("resolved entity is not matched with the principal")
	}

	signatureBytes, err = hex.DecodeString(passportDoc.Entity.AffiliationSignature)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode affiliation signature")
	}

	err = VerifySignature([]byte(passportDoc.Entity.AffiliationDocument), signatureBytes, principal)
	if err != nil {
		return nil, errors.Wrap(err, "failed to verify affiliation signature")
	}

	var affiliation AffiliationDocument
	err = json.Unmarshal([]byte(passportDoc.Entity.AffiliationDocument), &affiliation)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal affiliation document")
	}

	if affiliation.Domain != passportDoc.Domain {
		return nil, errors.New("domain is not matched with the passport")
	}

	return &VerifyJWTResult{
		Principal:       principal,
		Audience:        claims.Audience,
		Subject:         claims.Subject,
		Domain:          passportDoc.Domain,
		DocumentSigner:  passportDoc.Signer,
		AffiliationDate: affiliation.SignedAt,
	}, nil
}
