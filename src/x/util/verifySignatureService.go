package util

import (
    "log"
	"encoding/hex"
    "github.com/pkg/errors"
	"golang.org/x/crypto/sha3"
	"github.com/ethereum/go-ethereum/crypto"
)

func VerifySignature(message string, pubkeyStr string, signature_r string, signature_s string) error {

    log.Print(message)
    log.Print(pubkeyStr)
    log.Print(signature_r)
    log.Print(signature_s)

	// 公開鍵をデコード
	pubKeyBytes, err := hex.DecodeString(pubkeyStr)
	if err != nil {
        return err
	}

    // R値とS値をbig.Intに変換
	sigBytes, err := hex.DecodeString(signature_r + signature_s)
	if err != nil {
        return err
	}

	// メッセージをKeccak256でハッシュ化
	hash := sha3.NewLegacyKeccak256()
	hash.Write([]byte(message))
	hashedMessage := hash.Sum(nil)

	verified := crypto.VerifySignature(pubKeyBytes, hashedMessage, sigBytes)
    if verified {
        return nil
    }

    return errors.New("signature validation failed")
}

