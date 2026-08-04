package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/go-bip39"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/schemas"
)

type generatedIdentity struct {
	CCID       string
	Mnemonic   string
	PrivateKey string
	PublicKey  string
}

func generateIdentityMaterial() (generatedIdentity, error) {
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return generatedIdentity{}, fmt.Errorf("failed to generate entropy: %w", err)
	}

	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return generatedIdentity{}, fmt.Errorf("failed to generate mnemonic: %w", err)
	}

	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")
	if err != nil {
		return generatedIdentity{}, fmt.Errorf("failed to derive seed from mnemonic: %w", err)
	}

	master, ch := hd.ComputeMastersFromSeed(seed)
	priv, err := hd.DerivePrivateKeyForPath(master, ch, "m/44'/118'/0'/0/0")
	if err != nil {
		return generatedIdentity{}, fmt.Errorf("failed to derive private key: %w", err)
	}

	privKey := &secp256k1.PrivKey{Key: priv}
	pubKey := privKey.PubKey()
	fa := sdk.AccAddress(pubKey.Address())

	addrCdc := address.NewBech32Codec("con")
	addrStr, err := addrCdc.BytesToString(fa)
	if err != nil {
		return generatedIdentity{}, fmt.Errorf("failed to encode CCID: %w", err)
	}

	return generatedIdentity{
		CCID:       addrStr,
		Mnemonic:   mnemonic,
		PrivateKey: hex.EncodeToString(priv),
		PublicKey:  hex.EncodeToString(pubKey.Bytes()),
	}, nil
}

func identityFromPrivateKey(privKeyHex string) (generatedIdentity, error) {
	privKeyHex = strings.TrimSpace(privKeyHex)

	ccid, err := concrnt.PrivKeyToAddr(privKeyHex, "con")
	if err != nil {
		return generatedIdentity{}, fmt.Errorf("invalid private key: %w", err)
	}

	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return generatedIdentity{}, fmt.Errorf("invalid private key: %w", err)
	}

	privKey := &secp256k1.PrivKey{Key: privKeyBytes}

	return generatedIdentity{
		CCID:       ccid,
		PrivateKey: privKeyHex,
		PublicKey:  hex.EncodeToString(privKey.PubKey().Bytes()),
	}, nil
}

func normalizeCreateAccountAlias(alias string) *string {
	trimmed := strings.TrimSpace(alias)
	trimmed = strings.TrimPrefix(trimmed, "@")
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeCreateAccountInviter(inviter string) (*string, error) {
	trimmed := strings.TrimSpace(inviter)
	if trimmed == "" {
		return nil, nil
	}

	if !concrnt.IsCCID(trimmed) {
		return nil, fmt.Errorf("inviter must be a valid CCID: %s", trimmed)
	}

	return &trimmed, nil
}

func normalizeCreateAccountInfo(info string) (string, error) {
	trimmed := strings.TrimSpace(info)
	if trimmed == "" {
		trimmed = "null"
	}

	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return "", fmt.Errorf("info must be valid JSON: %w", err)
	}

	normalized, err := json.Marshal(parsed)
	if err != nil {
		return "", fmt.Errorf("failed to normalize info JSON: %w", err)
	}

	return string(normalized), nil
}

func buildCreateAccountRequest(
	identity generatedIdentity,
	fqdn string,
	alias *string,
	info string,
	createdAt time.Time,
) (concrnt.RegisterRequest, error) {
	if fqdn == "" {
		return concrnt.RegisterRequest{}, fmt.Errorf("fqdn cannot be empty")
	}

	createdAt = createdAt.UTC()

	document := concrnt.Document[schemas.Entity]{
		Kind: "entity",
		Key:  concrnt.ComposeCCURI("cckv", identity.CCID, ""),
		Value: schemas.Entity{
			Domain: fqdn,
			Alias:  alias,
		},
		Author:    identity.CCID,
		Schema:    schemas.EntityURL,
		CreatedAt: createdAt,
	}

	documentBytes, err := json.Marshal(document)
	if err != nil {
		return concrnt.RegisterRequest{}, fmt.Errorf("failed to marshal entity document: %w", err)
	}

	signatureBytes, err := concrnt.SignBytes(documentBytes, identity.PrivateKey)
	if err != nil {
		return concrnt.RegisterRequest{}, fmt.Errorf("failed to sign entity document: %w", err)
	}

	if err := concrnt.VerifySignature(documentBytes, signatureBytes, identity.CCID); err != nil {
		return concrnt.RegisterRequest{}, fmt.Errorf("failed to verify generated entity signature: %w", err)
	}

	signature := hex.EncodeToString(signatureBytes)

	return concrnt.RegisterRequest{
		SignedDocument: concrnt.SignedDocument{
			Document: string(documentBytes),
			Proof: concrnt.Proof{
				Type:      concrnt.ProofTypeEcrecover,
				Signature: &signature,
			},
		},
		Meta: json.RawMessage(info),
	}, nil
}
