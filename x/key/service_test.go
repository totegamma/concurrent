package key

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/core/mock"
	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/util"
)

const (
	RootKey  = "con1mu9xruulec4y6hd0d369sdf325l94z4770m33d"
	RootPriv = "3fcfac6c211b743975de2d7b3f622c12694b8125daf4013562c5a1aefa3253a5"

	SubKey1  = "cck1ydda2qj3nr32hulm65vj2g746f06hy36wzh9ke"
	SubPriv1 = "1ca30329e8d35217b2328bacfc21c5e3d762713edab0252eead1f4c1ac0b4d81"

	SubKey2  = "cck1evfmmesj9tn7ma8pvdufy4jhms66yk3xtg3hlz"
	SubPriv2 = "356fa07cf047fd7bc7f2c6e3869c024b7c4dd3378f708115a39b071189b8ccf9"
)

func TestService(t *testing.T) {

	var ctx = context.Background()

	db, cleanup_db := testutil.CreateDB()
	defer cleanup_db()

	mc, cleanup_mc := testutil.CreateMC()
	defer cleanup_mc()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEntity := mock_core.NewMockEntityService(ctrl)
	mockEntity.EXPECT().Get(gomock.Any(), gomock.Any()).Return(core.Entity{}, nil).AnyTimes()
	mockEntity.EXPECT().GetWithHint(gomock.Any(), gomock.Any(), gomock.Any()).Return(core.Entity{}, nil).AnyTimes()

	test_repo := NewRepository(db, mc)
	test_service := NewService(test_repo, mockEntity, core.Config{})

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

	err = test_service.ValidateDocument(ctx, objStr0, objSig0Hex, []core.Key{})
	assert.Error(t, err) // まだKeyChainが存在しないのでエラーになる

	// Test2. サブキーを新しく登録する
	payload1 := core.EnactDocument{
		DocumentBase: core.DocumentBase[any]{
			Signer:   RootKey,
			Type:     "enact",
			SignedAt: time.Now(),
		},
		Target: SubKey1,
		Root:   RootKey,
		Parent: RootKey,
	}

	objb1, err := json.Marshal(payload1)
	assert.NoError(t, err)

	objStr1 := string(objb1)
	objSig1, err := util.SignBytes(objb1, RootPriv)
	assert.NoError(t, err)
	objSig1Hex := hex.EncodeToString(objSig1)

	created, err := test_service.Enact(ctx, core.CommitModeExecute, objStr1, objSig1Hex)
	if assert.NoError(t, err) {
		assert.NotNil(t, created)
		assert.True(t, !created.ValidSince.IsZero())
		assert.True(t, created.ValidUntil.IsZero())
	}

	// Test3. 登録したサブキーで署名されたオブジェクトを検証する
	err = test_service.ValidateDocument(ctx, objStr0, objSig0Hex, []core.Key{})
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

	payload2 := core.EnactDocument{
		DocumentBase: core.DocumentBase[any]{
			Signer:   RootKey,
			Type:     "enact",
			KeyID:    SubKey1,
			SignedAt: time.Now(),
		},
		Target: SubKey2,
		Root:   RootKey,
		Parent: SubKey1,
	}

	objb2, err := json.Marshal(payload2)
	assert.NoError(t, err)

	objStr2 := string(objb2)
	objSig2, err := util.SignBytes(objb2, SubPriv1)
	assert.NoError(t, err)
	objSig2Hex := hex.EncodeToString(objSig2)

	created2, err := test_service.Enact(ctx, core.CommitModeExecute, objStr2, objSig2Hex)
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

	err = test_service.ValidateDocument(ctx, objStr3, objSig3Hex, []core.Key{})
	assert.NoError(t, err)

	// Test6. 中間のサブキーをその子キーから無効化してみようとする(失敗する)

	payload4 := core.RevokeDocument{
		DocumentBase: core.DocumentBase[any]{
			Signer:   RootKey,
			Type:     "revoke",
			KeyID:    SubKey2,
			SignedAt: time.Now(),
		},
		Target: SubKey1,
	}

	objb4, err := json.Marshal(payload4)
	assert.NoError(t, err)

	objStr4 := string(objb4)
	objSig4, err := util.SignBytes(objb4, RootPriv)
	assert.NoError(t, err)
	objSig4Hex := hex.EncodeToString(objSig4)

	_, err = test_service.Revoke(ctx, core.CommitModeExecute, objStr4, objSig4Hex)
	assert.Error(t, err)

	// Test7. 中間にあるサブキーをルートキーから無効化する

	payload5 := core.RevokeDocument{
		DocumentBase: core.DocumentBase[any]{
			Signer:   RootKey,
			Type:     "revoke",
			SignedAt: time.Now(),
		},
		Target: SubKey1,
	}

	objb5, err := json.Marshal(payload5)
	assert.NoError(t, err)

	objStr5 := string(objb5)
	objSig5, err := util.SignBytes(objb5, RootPriv)
	objSig5Hex := hex.EncodeToString(objSig5)
	assert.NoError(t, err)

	revoked, err := test_service.Revoke(ctx, core.CommitModeExecute, objStr5, objSig5Hex)
	if assert.NoError(t, err) {
		assert.True(t, !revoked.ValidSince.IsZero())
		assert.True(t, !revoked.ValidUntil.IsZero())
	}

	// Test8. 無効化したサブキーで署名されたオブジェクトを検証する(失敗する)

	err = test_service.ValidateDocument(ctx, objStr0, objSig0Hex, []core.Key{})
	assert.Error(t, err)

	// Test9. 無効化したサブキーのサブキーで署名されたオブジェクトを検証する(失敗する)

	err = test_service.ValidateDocument(ctx, objStr3, objSig3Hex, []core.Key{})
	assert.Error(t, err)

	_, err = test_service.ResolveSubkey(ctx, SubKey2)
	assert.Error(t, err)

	_, err = test_service.ResolveSubkey(ctx, SubKey1)
	assert.Error(t, err)

}
