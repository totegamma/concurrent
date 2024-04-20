package auth

import (
	"log"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"

	"github.com/totegamma/concurrent/internal/testutil"
	"github.com/totegamma/concurrent/x/domain/mock"
	"github.com/totegamma/concurrent/x/entity/mock"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key/mock"
)

const (
	User1ID   = "con1mu9xruulec4y6hd0d369sdf325l94z4770m33d"
	User1Priv = "3fcfac6c211b743975de2d7b3f622c12694b8125daf4013562c5a1aefa3253a5"

	SubKey1ID   = "cck1ydda2qj3nr32hulm65vj2g746f06hy36wzh9ke"
	SubKey1Priv = "1ca30329e8d35217b2328bacfc21c5e3d762713edab0252eead1f4c1ac0b4d81"
)

func TestIdentifyLocalIdentity(t *testing.T) {
	checker := testutil.SetupMockTraceProvider(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEntity := mock_entity.NewMockService(ctrl)
	mockEntity.EXPECT().Get(gomock.Any(), gomock.Any()).Return(core.Entity{
		ID:     User1ID,
		Domain: "example.com",
	}, nil).AnyTimes()
	mockDomain := mock_domain.NewMockService(ctrl)
	mockKey := mock_key.NewMockService(ctrl)

	config := util.Config{
		Concurrent: util.Concurrent{
			FQDN: "example.com",
		},
	}

	service := NewService(config, mockEntity, mockDomain, mockKey)

	c, req, rec, traceID := testutil.CreateHttpRequest()

	claims := jwt.Claims{
		Issuer:   User1ID,
		Subject:  "concrnt",
		Audience: "example.com",
	}

	jwt, err := jwt.Create(claims, User1Priv)
	if !assert.NoError(t, err) {
		log.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+jwt)

	h := service.IdentifyIdentity(func(c echo.Context) error {
		return nil
	})

	err = h(c)
	log.Println(rec.Body.String())
	if assert.NoError(t, err) {
		assert.Equal(t, core.LocalUser, c.Get(core.RequesterTypeCtxKey))
		assert.Equal(t, User1ID, c.Get(core.RequesterIdCtxKey))
		tags := c.Get(core.RequesterTagCtxKey).(core.Tags)
		tagString := tags.ToString()
		assert.Equal(t, "", tagString)
		assert.Equal(t, nil, c.Get(core.RequesterDomainCtxKey))
		assert.Equal(t, nil, c.Get(core.RequesterDomainTagsKey))
		assert.Equal(t, nil, c.Get(core.RequesterKeychainKey))
		assert.Equal(t, nil, c.Get(core.CaptchaVerifiedKey))
	}

	testutil.PrintSpans(checker.GetSpans(), traceID)
}
