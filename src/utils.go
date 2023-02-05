package main

import (
    "crypto"
    "crypto/rsa"
    "crypto/x509"
    "encoding/base64"
)

func verifySignature(message string, keystr string, signature string) error {
    // PEMの中身はDERと同じASN.1のバイナリデータをBase64によってエンコーディングされたテキストなのでBase64でデコードする
    // ゆえにDERエンコード形式に変換
    keyBytes, err := base64.StdEncoding.DecodeString(keystr)
    if err != nil {
        return err
    }

    // DERでエンコードされた公開鍵を解析する
    // 成功すると、pubは* rsa.PublicKey、* dsa.PublicKey、または* ecdsa.PublicKey型になる
    pub, err := x509.ParsePKIXPublicKey(keyBytes)
    if err != nil {
        return err
    }

    // 署名文字列はBase64でエンコーディングされたテキストなのでBase64でデコードする
    signDataByte, err := base64.StdEncoding.DecodeString(signature)
    if err != nil {
        return err
    }

    // SHA-256のハッシュ関数を使って受信データのハッシュ値を算出する
    h := crypto.Hash.New(crypto.SHA256)
    h.Write([]byte(message))
    hashed := h.Sum(nil)

    // 署名の検証、有効な署名はnilを返すことによって示される
    // ここで何をしているかというと、、
    // ①送信者のデータ（署名データ）を公開鍵で復号しハッシュ値を算出
    // ②受信側で算出したハッシュ値と、①のハッシュ値を比較し、一致すれば、「送信者が正しい」「データが改ざんされていない」ということを確認できる
    err = rsa.VerifyPKCS1v15(pub.(*rsa.PublicKey), crypto.SHA256, hashed, signDataByte)
    if err != nil {
        return err
    }

    return nil
}

