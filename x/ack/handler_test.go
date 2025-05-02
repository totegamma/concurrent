package ack

import (
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

	mockService := mock_core.NewMockAckService(ctrl)
	h := NewHandler(mockService)
	assert.NotNil(t, h)
	assert.Implements(t, (*Handler)(nil), h)
}

func TestHandler_GetAcker(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAckService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("test-acker-id")

	expectedAcks := []core.Ack{
		{Document: "doc1", Signature: "sig1", From: "userA", To: "test-acker-id", Valid: true},
	}

	// Success case
	mockService.EXPECT().
		GetAcker(gomock.Any(), "test-acker-id").
		Return(expectedAcks, nil).Times(1)

	if assert.NoError(t, h.GetAcker(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "ok", response["status"])
		// Further checks on response["content"] if needed, comparing with expectedAcks
	}

	// Error case
	rec = httptest.NewRecorder() // Reset recorder
	c = e.NewContext(req, rec)   // Reset context
	c.SetParamNames("id")
	c.SetParamValues("test-acker-id-fail")

	mockService.EXPECT().
		GetAcker(gomock.Any(), "test-acker-id-fail").
		Return(nil, errors.New("service error")).Times(1)

	// Check HTTP response instead of expecting direct error return
	err := h.GetAcker(c)
	assert.NoError(t, err) // Handler itself returns nil after writing response
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var response map[string]interface{}
	jsonErr := json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, jsonErr)
	assert.Contains(t, response["error"], "service error")

}

func TestHandler_GetAcking(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAckService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("test-acking-id")

	expectedAcks := []core.Ack{
		{Document: "doc2", Signature: "sig2", From: "test-acking-id", To: "userB", Valid: true},
	}

	// Success case
	mockService.EXPECT().
		GetAcking(gomock.Any(), "test-acking-id").
		Return(expectedAcks, nil).Times(1)

	if assert.NoError(t, h.GetAcking(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "ok", response["status"])
		// Further checks on response["content"] if needed
	}

	// Error case
	rec = httptest.NewRecorder() // Reset recorder
	c = e.NewContext(req, rec)   // Reset context
	c.SetParamNames("id")
	c.SetParamValues("test-acking-id-fail")

	mockService.EXPECT().
		GetAcking(gomock.Any(), "test-acking-id-fail").
		Return(nil, errors.New("service error")).Times(1)

	// Check HTTP response instead of expecting direct error return
	err := h.GetAcking(c)
	assert.NoError(t, err) // Handler itself returns nil after writing response
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var response map[string]interface{}
	jsonErr := json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, jsonErr)
	assert.Contains(t, response["error"], "service error")
}
