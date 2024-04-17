package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/util"

	"github.com/totegamma/concurrent/x/domain/mock"
	"github.com/totegamma/concurrent/x/entity/mock"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key/mock"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	User1ID   = "con1mu9xruulec4y6hd0d369sdf325l94z4770m33d"
	User1Priv = "3fcfac6c211b743975de2d7b3f622c12694b8125daf4013562c5a1aefa3253a5"

	SubKey1ID   = "cck1ydda2qj3nr32hulm65vj2g746f06hy36wzh9ke"
	SubKey1Priv = "1ca30329e8d35217b2328bacfc21c5e3d762713edab0252eead1f4c1ac0b4d81"
)

func SetupMockTraceProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	spanChecker := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanChecker))
	otel.SetTracerProvider(provider)

	return spanChecker
}

func printJson(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	log.Println(string(b))
}

func TestIdentifyLocalIdentity(t *testing.T) {

	checker := SetupMockTraceProvider(t)

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

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec)
	ctx, span := tracer.Start(c.Request().Context(), "testRoot")
	defer span.End()
	c.SetRequest(c.Request().WithContext(ctx))
	traceID := span.SpanContext().TraceID().String()

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

	PrintSpans(checker.GetSpans(), traceID)

}

func PrintSpans(spans tracetest.SpanStubs, traceID string) {
	fmt.Print("--------------------------------\n")
	for _, span := range spans {
		if !(span.SpanContext.TraceID().String() == traceID) {
			continue
		}

		fmt.Printf("Name: %s\n", span.Name)
		fmt.Printf("TraceID: %s\n", span.SpanContext.TraceID().String())
		fmt.Printf("Attributes:\n")
		for _, attr := range span.Attributes {
			fmt.Printf("  %s: %s: %s\n", attr.Key, attr.Value.Type().String(), attr.Value.AsString())
		}
		fmt.Printf("Events:\n")
		for _, event := range span.Events {
			fmt.Printf("  %s\n", event.Name)
			for _, attr := range event.Attributes {
				fmt.Printf("    %s: %s: %s\n", attr.Key, attr.Value.Type().String(), attr.Value.AsString())
			}
		}
		fmt.Print("--------------------------------\n")
	}

}
