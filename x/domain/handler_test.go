package domain

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/concrnt/concrnt/core"
	mock_core "github.com/concrnt/concrnt/core/mock"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockDomainService(ctrl)
	h := NewHandler(mockService)
	assert.NotNil(t, h)
	assert.Implements(t, (*Handler)(nil), h)
}

func TestHandler_Get(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockDomainService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("test.example.com")

	expectedDomain := core.Domain{
		ID:           "test.example.com",
		CCID:         "con1testccid",
		CSID:         "con1testcsid",
		Tag:          "testTag",
		Score:        100,
		IsScoreFixed: false,
		CDate:        time.Now().Add(-time.Hour),
		MDate:        time.Now(),
		LastScraped:  time.Now().Add(-time.Minute),
	}

	// Success case
	mockService.EXPECT().
		Get(gomock.Any(), "test.example.com").
		Return(expectedDomain, nil).Times(1)

	if assert.NoError(t, h.Get(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "ok", response["status"])
	}

	// Not Found case
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("notfound.example.com")

	mockService.EXPECT().
		Get(gomock.Any(), "notfound.example.com").
		Return(core.Domain{}, core.ErrorNotFound).Times(1)

	err := h.Get(c)
	assert.NoError(t, err) // Handler writes response and returns nil
	assert.Equal(t, http.StatusNotFound, rec.Code)
	var responseNotFound map[string]interface{}
	jsonErr := json.Unmarshal(rec.Body.Bytes(), &responseNotFound)
	assert.NoError(t, jsonErr)
	assert.Contains(t, responseNotFound["error"], "Domain not found")

	// Internal Server Error case
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("error.example.com")

	mockService.EXPECT().
		Get(gomock.Any(), "error.example.com").
		Return(core.Domain{}, errors.New("internal error")).Times(1)

	err = h.Get(c)
	assert.NoError(t, err) // Handler writes response and returns nil
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var responseError map[string]interface{}
	jsonErr = json.Unmarshal(rec.Body.Bytes(), &responseError)
	assert.NoError(t, jsonErr)
	assert.Contains(t, responseError["error"], "internal error")
}

func TestHandler_List(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockDomainService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/list", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	expectedDomains := []core.Domain{
		{ID: "domain1.example.com"},
		{ID: "domain2.example.com"},
	}

	// Success case
	mockService.EXPECT().
		List(gomock.Any()).
		Return(expectedDomains, nil).Times(1)

	if assert.NoError(t, h.List(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "ok", response["status"])
	}

	// Error case
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)

	mockService.EXPECT().
		List(gomock.Any()).
		Return(nil, errors.New("service error")).Times(1)

	err := h.List(c)
	assert.NoError(t, err) // Handler writes response and returns nil
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var responseError map[string]interface{}
	jsonErr := json.Unmarshal(rec.Body.Bytes(), &responseError)
	assert.NoError(t, jsonErr)
	assert.Contains(t, responseError["error"], "service error")
}
