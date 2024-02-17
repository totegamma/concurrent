package key

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"

	"github.com/totegamma/concurrent/internal/testutil"
)

const (
	RootKey  = "CCEb2a97367206f47407f5954Ced776633e394473F"
	RootPriv = "a46bbb4efd7ddb1d8a7a1a7a04b235452894e2b62d83a154fb5f61a991152fe0"

	SubKey1  = "CK0CC7d69558e666DA7ce6e5A7462acac9b47899a1"
	SubPriv1 = "5ad3b4247bf566c6faff61ea7340b2e967f05247147da8b3c3fdb108289ea01b"

	SubKey2  = "CK0174801A6a54A6f5631F48D707fb415Cdd120F4A"
	SubPriv2 = "a775d9a239d4b783153c89fec00d9c05010465b29bb5aee586209a72b3b5aee0"
)

func TestService(t *testing.T) {

	var ctx = context.Background()

	db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()

	test_repo := NewRepository(db)
	test_service := NewService(test_repo, util.Config{})

	// Test1. 登録してないサブキーで署名されたオブジェクトを検証する
	payload0 := core.SignedObject[any]{
		Signer: RootKey,
		Type:   "dummy",
		KeyID:  SubKey1,
	}

	objb0, err := json.Marshal(payload0)
	if err != nil {
		panic(err)
	}

	objStr0 := string(objb0)
	objSig0, err := util.SignBytes(objb0, SubPriv1)
	if err != nil {
		panic(err)
	}

	err = test_service.ValidateSignedObject(ctx, objStr0, objSig0)
	assert.Error(t, err) // まだKeyChainが存在しないのでエラーになる

	// Test2. サブキーを新しく登録する
	payload1 := core.SignedObject[core.Enact]{
		Signer: RootKey,
		Type:   "enact",
		Body: core.Enact{
			CKID:   SubKey1,
			Root:   RootKey,
			Parent: RootKey,
		},
	}

	objb1, err := json.Marshal(payload1)
	if err != nil {
		panic(err)
	}

	objStr1 := string(objb1)
	objSig1, err := util.SignBytes(objb1, RootPriv)
	if err != nil {
		panic(err)
	}

	created, err := test_service.EnactKey(ctx, objStr1, objSig1)
	if assert.NoError(t, err) {
		assert.NotNil(t, created)
	}

	// Test3. 登録したサブキーで署名されたオブジェクトを検証する
	err = test_service.ValidateSignedObject(ctx, objStr0, objSig0)
	assert.NoError(t, err)

	// test3.1 test GetKeyResolution
	resolution0, err := test_service.GetKeyResolution(ctx, SubKey1)
	if assert.NoError(t, err) {
		assert.Len(t, resolution0, 1)
	}

	// Test3.2 サブキーからルートキーを解決する
	root0, err := test_service.ResolveSubkey(ctx, SubKey1)
	if assert.NoError(t, err) {
		assert.Equal(t, RootKey, root0)
	}

	// Test4. サブキーのサブキーを新しく登録する

	payload2 := core.SignedObject[core.Enact]{
		Signer: SubKey2,
		Type:   "enact",
		Body: core.Enact{
			CKID:   SubKey2,
			Root:   RootKey,
			Parent: SubKey1,
		},
		KeyID: SubKey1,
	}

	objb2, err := json.Marshal(payload2)
	if err != nil {
		panic(err)
	}

	objStr2 := string(objb2)
	objSig2, err := util.SignBytes(objb2, SubPriv1)
	if err != nil {
		panic(err)
	}

	created2, err := test_service.EnactKey(ctx, objStr2, objSig2)
	if assert.NoError(t, err) {
		assert.NotNil(t, created2)
	}

	// test4.1 test GetKeyResolution
	resolution1, err := test_service.GetKeyResolution(ctx, SubKey2)
	if assert.NoError(t, err) {
		assert.Len(t, resolution1, 2)
	}

	// Test4.2 サブキーからルートキーを解決する
	root1, err := test_service.ResolveSubkey(ctx, SubKey2)
	if assert.NoError(t, err) {
		assert.Equal(t, RootKey, root1)
	}

	// Test5. 登録したサブキーのサブキーで署名されたオブジェクトを検証する

	payload3 := core.SignedObject[any]{
		Signer: RootKey,
		Type:   "dummy",
		KeyID:  SubKey2,
	}

	objb3, err := json.Marshal(payload3)
	if err != nil {
		panic(err)
	}

	objStr3 := string(objb3)
	objSig3, err := util.SignBytes(objb3, SubPriv2)
	if err != nil {
		panic(err)
	}

	err = test_service.ValidateSignedObject(ctx, objStr3, objSig3)
	assert.NoError(t, err)

	// Test6. 中間のサブキーをその子キーから無効化してみようとする(失敗する)

	payload4 := core.SignedObject[core.Revoke]{
		Signer: RootKey,
		Type:   "revoke",
		Body: core.Revoke{
			CKID: SubKey1,
		},
		KeyID: SubKey2,
	}

	objb4, err := json.Marshal(payload4)
	if err != nil {
		panic(err)
	}

	objStr4 := string(objb4)
	objSig4, err := util.SignBytes(objb4, RootPriv)
	if err != nil {
		panic(err)
	}

	_, err = test_service.RevokeKey(ctx, objStr4, objSig4)
	assert.Error(t, err)

	// Test7. 中間にあるサブキーをルートキーから無効化する

	payload5 := core.SignedObject[core.Revoke]{
		Signer: RootKey,
		Type:   "revoke",
		Body: core.Revoke{
			CKID: SubKey1,
		},
	}

	objb5, err := json.Marshal(payload5)
	if err != nil {
		panic(err)
	}

	objStr5 := string(objb5)
	objSig5, err := util.SignBytes(objb5, RootPriv)
	if err != nil {
		panic(err)
	}

	_, err = test_service.RevokeKey(ctx, objStr5, objSig5)
	assert.NoError(t, err)

	// Test8. 無効化したサブキーで署名されたオブジェクトを検証する(失敗する)

	err = test_service.ValidateSignedObject(ctx, objStr0, objSig0)
	assert.Error(t, err)

	// Test9. 無効化したサブキーのサブキーで署名されたオブジェクトを検証する(失敗する)

	err = test_service.ValidateSignedObject(ctx, objStr3, objSig3)
	assert.Error(t, err)

	_, err = test_service.ResolveSubkey(ctx, SubKey2)
	assert.Error(t, err)

	_, err = test_service.ResolveSubkey(ctx, SubKey1)
	assert.Error(t, err)

}
