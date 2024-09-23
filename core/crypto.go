package core

import (
	"encoding/hex"
	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"gitlab.com/yawning/secp256k1-voi/secec"
	"golang.org/x/crypto/sha3"
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
