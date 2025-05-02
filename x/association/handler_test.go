package association

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	mockService := mock_core.NewMockAssociationService(ctrl)
	h := NewHandler(mockService)
	assert.NotNil(t, h)
	assert.Implements(t, (*Handler)(nil), h)
}

func TestHandler_Get(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAssociationService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("testAssocID")

	expectedAssoc := core.Association{
		ID:        "testAssocID",
		Author:    "con1author",
		Schema:    "testSchema",
		Target:    "targetID",
		Variant:   "testVariant",
		Signature: "sig",
		CDate:     time.Now(),
	}

	// Success case
	mockService.EXPECT().
		Get(gomock.Any(), "testAssocID").
		Return(expectedAssoc, nil).Times(1)

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
	c.SetParamValues("notFoundID")

	mockService.EXPECT().
		Get(gomock.Any(), "notFoundID").
		Return(core.Association{}, core.ErrorNotFound).Times(1)

	err := h.Get(c)
	assert.NoError(t, err) // Handler returns nil after writing response
	assert.Equal(t, http.StatusNotFound, rec.Code)
	var responseNotFound map[string]interface{}
	jsonErr := json.Unmarshal(rec.Body.Bytes(), &responseNotFound)
	assert.NoError(t, jsonErr)
	assert.Contains(t, responseNotFound["error"], "association not found")

	// Internal Server Error case
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("errorID")

	mockService.EXPECT().
		Get(gomock.Any(), "errorID").
		Return(core.Association{}, errors.New("internal error")).Times(1)

	err = h.Get(c)
	assert.NoError(t, err) // Handler returns nil after writing response
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var responseError map[string]interface{}
	jsonErr = json.Unmarshal(rec.Body.Bytes(), &responseError)
	assert.NoError(t, jsonErr)
	assert.Contains(t, responseError["error"], "internal error")
}

func TestHandler_GetAttached(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAssociationService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("targetMessageID")

	expectedAssocs := []core.Association{
		{ID: "assoc1", Target: "targetMessageID"},
		{ID: "assoc2", Target: "targetMessageID"},
	}

	// Success case
	mockService.EXPECT().
		GetByTarget(gomock.Any(), "targetMessageID").
		Return(expectedAssocs, nil).Times(1)

	if assert.NoError(t, h.GetAttached(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Error case
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("errorTargetID")

	mockService.EXPECT().
		GetByTarget(gomock.Any(), "errorTargetID").
		Return(nil, errors.New("service error")).Times(1)

	err := h.GetAttached(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandler_GetOwnByTarget(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAssociationService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("targetID")

	requesterID := "con1requester"
	ctx := context.WithValue(c.Request().Context(), core.RequesterIdCtxKey, requesterID)
	c.SetRequest(c.Request().WithContext(ctx))

	expectedAssocs := []core.Association{
		{ID: "assoc3", Target: "targetID", Author: requesterID},
	}

	// Success case
	mockService.EXPECT().
		GetOwnByTarget(gomock.Any(), "targetID", requesterID).
		Return(expectedAssocs, nil).Times(1)

	if assert.NoError(t, h.GetOwnByTarget(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Error case
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("errorTargetID")
	ctx = context.WithValue(c.Request().Context(), core.RequesterIdCtxKey, requesterID) // Reset context with requester
	c.SetRequest(c.Request().WithContext(ctx))

	mockService.EXPECT().
		GetOwnByTarget(gomock.Any(), "errorTargetID", requesterID).
		Return(nil, errors.New("service error")).Times(1)

	err := h.GetOwnByTarget(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandler_GetCounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAssociationService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()

	// Case 1: No schema query param (GetCountsBySchema)
	t.Run("GetCountsBySchema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("messageID1")

		expectedCounts := map[string]int64{"schemaA": 5, "schemaB": 10}

		mockService.EXPECT().
			GetCountsBySchema(gomock.Any(), "messageID1").
			Return(expectedCounts, nil).Times(1)

		if assert.NoError(t, h.GetCounts(c)) {
			assert.Equal(t, http.StatusOK, rec.Code)
		}

		// Error case
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("errorMsgID1")
		mockService.EXPECT().
			GetCountsBySchema(gomock.Any(), "errorMsgID1").
			Return(nil, errors.New("service error 1")).Times(1)
		err := h.GetCounts(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	// Case 2: With schema query param (GetCountsBySchemaAndVariant)
	t.Run("GetCountsBySchemaAndVariant", func(t *testing.T) {
		q := make(url.Values)
		q.Set("schema", "schemaA")
		req := httptest.NewRequest(http.MethodGet, "/?"+q.Encode(), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("messageID2")

		// Initialize directly as a map
		expectedCounts := &core.OrderedKVMap[int64]{
			"variant1": {Value: 2, Order: 1}, // Assign arbitrary order for test
			"variant2": {Value: 3, Order: 2},
		}

		mockService.EXPECT().
			GetCountsBySchemaAndVariant(gomock.Any(), "messageID2", "schemaA").
			Return(expectedCounts, nil).Times(1)

		if assert.NoError(t, h.GetCounts(c)) {
			assert.Equal(t, http.StatusOK, rec.Code)
		}

		// Error case
		rec = httptest.NewRecorder()
		c = e.NewContext(req, rec) // Need to recreate context with query params
		c.SetParamNames("id")
		c.SetParamValues("errorMsgID2")
		mockService.EXPECT().
			GetCountsBySchemaAndVariant(gomock.Any(), "errorMsgID2", "schemaA").
			Return(nil, errors.New("service error 2")).Times(1)
		err := h.GetCounts(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandler_GetFiltered(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock_core.NewMockAssociationService(ctrl)
	h := NewHandler(mockService)

	e := echo.New()
	targetID := "targetMessageID"
	schema := "schemaA"
	variant := "variant1"

	expectedAssoc1 := core.Association{ID: "assoc1", Target: targetID, Schema: schema, Variant: variant}
	expectedAssoc2 := core.Association{ID: "assoc2", Target: targetID, Schema: schema, Variant: "variant2"}
	expectedAssoc3 := core.Association{ID: "assoc3", Target: targetID, Schema: "schemaB"}

	// Case 1: No schema or variant (GetByTarget)
	t.Run("GetByTarget", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID)

		mockService.EXPECT().
			GetByTarget(gomock.Any(), targetID).
			Return([]core.Association{expectedAssoc1, expectedAssoc2, expectedAssoc3}, nil).Times(1)

		if assert.NoError(t, h.GetFiltered(c)) {
			assert.Equal(t, http.StatusOK, rec.Code)
		}
	})

	// Case 2: Schema only (GetBySchema)
	t.Run("GetBySchema", func(t *testing.T) {
		q := make(url.Values)
		q.Set("schema", schema)
		req := httptest.NewRequest(http.MethodGet, "/?"+q.Encode(), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID)

		mockService.EXPECT().
			GetBySchema(gomock.Any(), targetID, schema).
			Return([]core.Association{expectedAssoc1, expectedAssoc2}, nil).Times(1)

		if assert.NoError(t, h.GetFiltered(c)) {
			assert.Equal(t, http.StatusOK, rec.Code)
		}
	})

	// Case 3: Schema and Variant (GetBySchemaAndVariant)
	t.Run("GetBySchemaAndVariant", func(t *testing.T) {
		q := make(url.Values)
		q.Set("schema", schema)
		q.Set("variant", variant)
		req := httptest.NewRequest(http.MethodGet, "/?"+q.Encode(), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID)

		mockService.EXPECT().
			GetBySchemaAndVariant(gomock.Any(), targetID, schema, variant).
			Return([]core.Association{expectedAssoc1}, nil).Times(1)

		if assert.NoError(t, h.GetFiltered(c)) {
			assert.Equal(t, http.StatusOK, rec.Code)
		}
	})
}
