package util

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

const (
	RootKey  = "con1fk8zlkrfmens3sgj7dzcu3gsw8v9kkysrf8dt5"
	RootPriv = "1236fa65392e99067750aaed5fd4d9ff93f51fd088e94963e51669396cdd597c"
	RootPub  = "020bb249a8bb7a10defe954abba5a4320cabb6c49513bfaf6b204ca8c4e4248c01"
)

func TestSignature(t *testing.T) {
	message := "hello"

	signature, err := SignBytes([]byte(message), RootPriv)
	assert.NoError(t, err)

	err = VerifySignature([]byte(message), signature, RootPub)
	assert.NoError(t, err)
}
