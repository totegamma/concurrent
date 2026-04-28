package main

import (
	"fmt"

	"encoding/hex"

	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/go-bip39"
	"github.com/spf13/cobra"
)

var identityCmd = &cobra.Command{
	Use:   "identity",
	Short: "Generate a new concrnt identity",
	Run: func(cmd *cobra.Command, args []string) {

		entropy, err := bip39.NewEntropy(128)

		mnemonic, err := bip39.NewMnemonic(entropy)
		if err != nil {
			panic(err)
		}

		seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")
		if err != nil {
			panic(err)
		}
		master, ch := hd.ComputeMastersFromSeed(seed)
		priv, err := hd.DerivePrivateKeyForPath(master, ch, "m/44'/118'/0'/0/0")
		if err != nil {
			panic(err)
		}

		privHex := hex.EncodeToString(priv)

		privKey := &secp256k1.PrivKey{Key: priv}

		// get the public key
		pubKey := privKey.PubKey()
		pubKeyHex := hex.EncodeToString(pubKey.Bytes())

		fa := sdk.AccAddress(pubKey.Address())

		// convert the address to string
		addrCdc := address.NewBech32Codec("con")
		addrStr, err := addrCdc.BytesToString(fa)
		if err != nil {
			panic(err)
		}

		fmt.Println("ccid:\t\t", addrStr)
		fmt.Println("mnemonic:\t", mnemonic)
		fmt.Println("privatekey:\t", privHex)
		fmt.Println("publickey:\t", pubKeyHex)

	},
}

func init() {
	generateCmd.AddCommand(identityCmd)
}
