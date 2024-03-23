package util

import (
	"encoding/hex"
	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
)

var (
	cdc = address.NewBech32Codec("con")
)

func SignBytes(bytes []byte, privatekey string) ([]byte, error) {

	privateKeyBytes, err := hex.DecodeString(privatekey)
	if err != nil {
		return []byte{}, errors.Wrap(err, "failed to decode private key")
	}

	privKey := secp256k1.PrivKey{
		Key: privateKeyBytes,
	}

	// sign
	signature, err := privKey.Sign(bytes)
	if err != nil {
		return []byte{}, err
	}

	return signature, nil
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

func PubkeyToAddr(pubkeyHex string) (string, error) {
	pubKeyBytes, err := hex.DecodeString(pubkeyHex)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode public key")
	}

	pubkey := secp256k1.PubKey{
		Key: pubKeyBytes,
	}

	account := sdk.AccAddress(pubkey.Address())
	addr, err := cdc.BytesToString(account)
	if err != nil {
		return "", errors.Wrap(err, "failed to convert address")
	}

	return addr, nil
}

func VerifyPubkey(pubkey string, address string) error {
	addr, err := PubkeyToAddr(pubkey)
	if err != nil {
		return errors.Wrap(err, "failed to convert pubkey to address")
	}

	if addr != address {
		return errors.New("pubkey is not matched with address")
	}

	return nil
}
