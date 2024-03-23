package key

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/entity/mock"
	"github.com/totegamma/concurrent/x/util"
)

const (
	RootKey  = "con1fk8zlkrfmens3sgj7dzcu3gsw8v9kkysrf8dt5"
	RootPriv = "1236fa65392e99067750aaed5fd4d9ff93f51fd088e94963e51669396cdd597c"
	RootPub  = "020bb249a8bb7a10defe954abba5a4320cabb6c49513bfaf6b204ca8c4e4248c01"

	SubKey1  = "cck1v26je8uyhc9x6xgcw26d3cne20s44atr7a94em"
	SubPriv1 = "958ca19f7b011dd101f698c87906750dc2bc20c99943de55c49ebdae668ca244"
	SubPub1  = "0258abc2cbd73a85c70e9fa4a9e66661e5fee20e3c29c8dd575d8dccc12ed958da"

	SubKey2  = "cck18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe"
	SubPriv2 = "f48627a9728c589263bba24c5feb72e5b0ebc8b342d5e75e055a17cc93115678"
	SubPub2  = "02dd4b17bf8738a597d01a86a45af704881493de8357aea775c9b605de6aed8493"
)

func TestService(t *testing.T) {

	var ctx = context.Background()

	db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()

	mc, cleanup_mc := testutil.CreateMC()
	defer cleanup_mc()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEntity := mock_entity.NewMockService(ctrl)
	mockEntity.EXPECT().ResolveHost(gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()
	mockEntity.EXPECT().Get(gomock.Any(), RootKey).Return(core.Entity{
		Pubkey: RootPub,
	}, nil).AnyTimes()
	mockEntity.EXPECT().Get(gomock.Any(), SubKey1).Return(core.Entity{
		Pubkey: SubPub1,
	}, nil).AnyTimes()
	mockEntity.EXPECT().Get(gomock.Any(), SubKey2).Return(core.Entity{
		Pubkey: SubPub2,
	}, nil).AnyTimes()

	test_repo := NewRepository(db, mc)
	test_service := NewService(test_repo, mockEntity, util.Config{})

	// Test1. 登録してないサブキーで署名されたオブジェクトを検証する
	payload0 := core.DocumentBase[any]{
		Signer:   RootKey,
		Type:     "dummy",
		KeyID:    SubKey1,
		SignedAt: time.Now(),
	}

	objb0, err := json.Marshal(payload0)
	assert.NoError(t, err)

	objStr0 := string(objb0)
	objSig0, err := util.SignBytes(objb0, SubPriv1)
	assert.NoError(t, err)

	objSig0Hex := hex.EncodeToString(objSig0)

	err = test_service.ValidateSignedObject(ctx, objStr0, objSig0Hex)
	assert.Error(t, err) // まだKeyChainが存在しないのでエラーになる

	// Test2. サブキーを新しく登録する
	payload1 := core.EnactKey{
		DocumentBase: core.DocumentBase[core.EnactBody]{
			Signer: RootKey,
			Type:   "enact",
			Body: core.EnactBody{
				CKID:   SubKey1,
				Pubkey: SubPub1,
				Root:   RootKey,
				Parent: RootKey,
			},
			SignedAt: time.Now(),
		},
	}

	objb1, err := json.Marshal(payload1)
	assert.NoError(t, err)

	objStr1 := string(objb1)
	objSig1, err := util.SignBytes(objb1, RootPriv)
	assert.NoError(t, err)
	objSig1Hex := hex.EncodeToString(objSig1)

	created, err := test_service.EnactKey(ctx, objStr1, objSig1Hex)
	if assert.NoError(t, err) {
		assert.NotNil(t, created)
		assert.True(t, !created.ValidSince.IsZero())
		assert.True(t, created.ValidUntil.IsZero())
	}

	// Test3. 登録したサブキーで署名されたオブジェクトを検証する
	err = test_service.ValidateSignedObject(ctx, objStr0, objSig0Hex)
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

	payload2 := core.EnactKey{
		DocumentBase: core.DocumentBase[core.EnactBody]{
			Signer: RootKey,
			Type:   "enact",
			Body: core.EnactBody{
				CKID:   SubKey2,
				Pubkey: SubPub2,
				Root:   RootKey,
				Parent: SubKey1,
			},
			KeyID:    SubKey1,
			SignedAt: time.Now(),
		},
	}

	objb2, err := json.Marshal(payload2)
	assert.NoError(t, err)

	objStr2 := string(objb2)
	objSig2, err := util.SignBytes(objb2, SubPriv1)
	assert.NoError(t, err)
	objSig2Hex := hex.EncodeToString(objSig2)

	created2, err := test_service.EnactKey(ctx, objStr2, objSig2Hex)
	if assert.NoError(t, err) {
		assert.NotNil(t, created2)
		assert.True(t, !created2.ValidSince.IsZero())
		assert.True(t, created2.ValidUntil.IsZero())
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

	payload3 := core.DocumentBase[any]{
		Signer:   RootKey,
		Type:     "dummy",
		KeyID:    SubKey2,
		SignedAt: time.Now(),
	}

	objb3, err := json.Marshal(payload3)
	assert.NoError(t, err)

	objStr3 := string(objb3)
	objSig3, err := util.SignBytes(objb3, SubPriv2)
	assert.NoError(t, err)
	objSig3Hex := hex.EncodeToString(objSig3)

	err = test_service.ValidateSignedObject(ctx, objStr3, objSig3Hex)
	assert.NoError(t, err)

	// Test6. 中間のサブキーをその子キーから無効化してみようとする(失敗する)

	payload4 := core.RevokeKey{
		DocumentBase: core.DocumentBase[core.RevokeBody]{
			Signer: RootKey,
			Type:   "revoke",
			Body: core.RevokeBody{
				CKID: SubKey1,
			},
			KeyID:    SubKey2,
			SignedAt: time.Now(),
		},
	}

	objb4, err := json.Marshal(payload4)
	assert.NoError(t, err)

	objStr4 := string(objb4)
	objSig4, err := util.SignBytes(objb4, RootPriv)
	assert.NoError(t, err)
	objSig4Hex := hex.EncodeToString(objSig4)

	_, err = test_service.RevokeKey(ctx, objStr4, objSig4Hex)
	assert.Error(t, err)

	// Test7. 中間にあるサブキーをルートキーから無効化する

	payload5 := core.RevokeKey{
		DocumentBase: core.DocumentBase[core.RevokeBody]{
			Signer: RootKey,
			Type:   "revoke",
			Body: core.RevokeBody{
				CKID: SubKey1,
			},
			SignedAt: time.Now(),
		},
	}

	objb5, err := json.Marshal(payload5)
	assert.NoError(t, err)

	objStr5 := string(objb5)
	objSig5, err := util.SignBytes(objb5, RootPriv)
	objSig5Hex := hex.EncodeToString(objSig5)
	assert.NoError(t, err)

	revoked, err := test_service.RevokeKey(ctx, objStr5, objSig5Hex)
	if assert.NoError(t, err) {
		assert.True(t, !revoked.ValidSince.IsZero())
		assert.True(t, !revoked.ValidUntil.IsZero())
	}

	// Test8. 無効化したサブキーで署名されたオブジェクトを検証する(失敗する)

	err = test_service.ValidateSignedObject(ctx, objStr0, objSig0Hex)
	assert.Error(t, err)

	// Test9. 無効化したサブキーのサブキーで署名されたオブジェクトを検証する(失敗する)

	err = test_service.ValidateSignedObject(ctx, objStr3, objSig3Hex)
	assert.Error(t, err)

	_, err = test_service.ResolveSubkey(ctx, SubKey2)
	assert.Error(t, err)

	_, err = test_service.ResolveSubkey(ctx, SubKey1)
	assert.Error(t, err)

}
