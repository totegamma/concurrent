package auth

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"
	"testing"
	// "github.com/totegamma/concurrent/x/entity"
	// "github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/internal/testutil"
)

// RootKey
// CCID: CCEb2a97367206f47407f5954Ced776633e394473F
// privateKey: a46bbb4efd7ddb1d8a7a1a7a04b235452894e2b62d83a154fb5f61a991152fe0

// SubKey
// CKID: CK0CC7d69558e666DA7ce6e5A7462acac9b47899a1
// privateKey: 5ad3b4247bf566c6faff61ea7340b2e967f05247147da8b3c3fdb108289ea01b

func TestService(t *testing.T) {

	var ctx = context.Background()

	db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()

	test_repo := NewRepository(db)
	test_service := NewService(test_repo, util.Config{}, nil, nil)

	payload := core.SignedObject[core.Enact]{
		Signer: "CCEb2a97367206f47407f5954Ced776633e394473F",
		Type:   "enact",
		Body: core.Enact{
			CKID:   "CK0CC7d69558e666DA7ce6e5A7462acac9b47899a1",
			Root:   "CCEb2a97367206f47407f5954Ced776633e394473F",
			Parent: "CCEb2a97367206f47407f5954Ced776633e394473F",
		},
	}

	objb, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	objStr := string(objb)
	objSig, err := util.SignBytes(objb, "a46bbb4efd7ddb1d8a7a1a7a04b235452894e2b62d83a154fb5f61a991152fe0")
	if err != nil {
		panic(err)
	}

	created, err := test_service.EnactKey(ctx, objStr, objSig)
	if assert.NoError(t, err) {
		assert.NotNil(t, created)
	}

}
