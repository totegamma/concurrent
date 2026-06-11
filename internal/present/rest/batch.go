package rest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func batchHandler(app *echo.Echo) echo.HandlerFunc {
	return func(c echo.Context) error {
		req := c.Request()
		ct := req.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(ct)
		if err != nil || !strings.EqualFold(mediaType, "multipart/mixed") {
			fmt.Println("Invalid Content-Type:", ct)
			return echo.NewHTTPError(http.StatusBadRequest, "Content-Type must be multipart/mixed")
		}
		boundary := params["boundary"]
		if boundary == "" {
			fmt.Println("Missing boundary in Content-Type")
			return echo.NewHTTPError(http.StatusBadRequest, "missing boundary")
		}

		mr := multipart.NewReader(req.Body, boundary)

		c.Response().Header().Set("Content-Type", "multipart/mixed; boundary="+boundary)
		mw := multipart.NewWriter(c.Response().Writer)
		mw.SetBoundary(boundary)
		defer mw.Close()

		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "failed to read multipart body")
			}

			contentType := part.Header.Get("Content-Type")
			if !strings.EqualFold(contentType, "application/http") {
				continue
			}
			contentID := part.Header.Get("Content-ID")

			reader := bufio.NewReader(part)

			pr, err := http.ReadRequest(reader)
			if err != nil {
				fmt.Println("Failed to parse part as HTTP request:", err)
				return echo.NewHTTPError(http.StatusBadRequest, "failed to parse part as HTTP request")
			}

			path := pr.URL.Path
			fmt.Println("Processing request for path:", path)

			recorder := NewResponseRecorder()

			app.ServeHTTP(recorder, pr)

			pw, err := mw.CreatePart(map[string][]string{
				"Content-Type": {"application/http"},
				"Content-ID":   {contentID},
			})
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to create multipart part")
			}

			result := recorder.Result()
			result.Write(pw)
		}

		return nil
	}
}

type responseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func NewResponseRecorder() *responseRecorder {
	return &responseRecorder{
		header: make(http.Header),
		status: 0,
	}
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
}

func (r *responseRecorder) Result() *http.Response {
	return &http.Response{
		StatusCode: r.status,
		Header:     r.header,
		Body:       io.NopCloser(&r.body),
	}
}
