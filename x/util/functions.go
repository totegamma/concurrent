package util

import (
	"encoding/hex"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/pkg/errors"
)

func SignBytes(bytes []byte, privatekey string) (string, error) {

	privateKeyBytes, err := hex.DecodeString(privatekey)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode private key")
	}

	privKey := secp256k1.PrivKey{
		Key: privateKeyBytes,
	}

	// sign
	signature, err := privKey.Sign(bytes)
	if err != nil {
		return "", err
	}

	// encode
	encoded := hex.EncodeToString(signature)
	return encoded, nil
}

func VerifySignature(message []byte, signature []byte, pubkey string) error {

	// decode pubkey
	pubKeyBytes, err := hex.DecodeString(pubkey)
	if err != nil {
		return errors.Wrap(err, "failed to decode public key")
	}

	pubKey := secp256k1.PubKey{
		Key: pubKeyBytes,
	}

	// verify
	if !pubKey.VerifySignature(message, signature) {
		return errors.New("signature validation failed")
	}

	return nil
}
