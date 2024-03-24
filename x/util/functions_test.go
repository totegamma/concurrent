package util

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

const (
	RootAddr    = "con1dxp8n8ctve3wq9gws3a92exj9p4nld2h6r838v"
	RootPrivKey = "f1229da9dbb71bbd6aaf3adef77f6463d5beb1f4c9d697e9d430ddd621597a6f"

	SubKey1  = "cck1ydda2qj3nr32hulm65vj2g746f06hy36wzh9ke"
	SubPriv1 = "1ca30329e8d35217b2328bacfc21c5e3d762713edab0252eead1f4c1ac0b4d81"
)

func TestSignature(t *testing.T) {
	message := "hello"

	signature, err := SignBytes([]byte(message), RootPrivKey)
	assert.NoError(t, err)

	err = VerifySignature([]byte(message), signature, RootAddr)
	assert.NoError(t, err)

	signature1, err := SignBytes([]byte(message), SubPriv1)
	assert.NoError(t, err)

	err = VerifySignature([]byte(message), signature1, SubKey1)
	assert.NoError(t, err)
}
