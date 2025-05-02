package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/concrnt/concrnt/core"
	mock_core "github.com/concrnt/concrnt/core/mock"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAuthService(ctrl)
	h := NewHandler(mockService)
	assert.NotNil(t, h)
	assert.Implements(t, (*Handler)(nil), h)
}

func TestHandler_GetPassport(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAuthService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/passport", nil) // Use a relevant path
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	requesterID := "con1requester"
	dummyKeys := []core.Key{{ID: "key1"}}
	expectedPassport := "dummy_passport_jwt"

	// --- Success Case ---
	t.Run("Success", func(t *testing.T) {
		rec = httptest.NewRecorder() // Reset recorder for each subtest
		c = e.NewContext(req, rec)   // Reset context

		// Set context values
		ctx := context.WithValue(c.Request().Context(), core.RequesterIdCtxKey, requesterID)
		ctx = context.WithValue(ctx, core.RequesterKeychainKey, dummyKeys)
		c.SetRequest(c.Request().WithContext(ctx))

		mockService.EXPECT().
			IssuePassport(gomock.Any(), requesterID, dummyKeys).
			Return(expectedPassport, nil).Times(1)

		if assert.NoError(t, h.GetPassport(c)) {
			assert.Equal(t, http.StatusOK, rec.Code)
			var response map[string]interface{}
			err := json.Unmarshal(rec.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, expectedPassport, response["content"])
		}
	})

	// --- Failure Case: Requester not found in context ---
	t.Run("Fail_NoRequester", func(t *testing.T) {
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec) // Context without requester ID

		// Service should not be called
		mockService.EXPECT().IssuePassport(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		err := h.GetPassport(c)
		assert.NoError(t, err) // Handler writes response and returns nil
		assert.Equal(t, http.StatusForbidden, rec.Code)
		var response map[string]interface{}
		jsonErr := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, jsonErr)
		assert.Contains(t, response["message"], "requester not found")
	})

	// --- Failure Case: Service Error ---
	t.Run("Fail_ServiceError", func(t *testing.T) {
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec)

		// Set context values
		ctx := context.WithValue(c.Request().Context(), core.RequesterIdCtxKey, requesterID)
		ctx = context.WithValue(ctx, core.RequesterKeychainKey, dummyKeys)
		c.SetRequest(c.Request().WithContext(ctx))

		mockService.EXPECT().
			IssuePassport(gomock.Any(), requesterID, dummyKeys).
			Return("", errors.New("service error")).Times(1)

		err := h.GetPassport(c)
		assert.NoError(t, err) // Handler writes response and returns nil
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		var response map[string]interface{}
		jsonErr := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, jsonErr)
		assert.Contains(t, response["error"], "service error")
	})
}
