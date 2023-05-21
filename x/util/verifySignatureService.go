// Package util provides various utility functions
package util

import (
    "encoding/hex"
    "github.com/pkg/errors"
    "golang.org/x/crypto/sha3"
    "github.com/ethereum/go-ethereum/crypto"
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

