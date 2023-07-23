package main

import (
	"github.com/labstack/echo/v4"
	"net/http/httputil"
	"net/url"
)


func main() {
	targetUrl, err := url.Parse("http://ccapi:8000")
	if err != nil {
		panic(err)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetUrl)

	e := echo.New()

	e.Any("/api/*", func(c echo.Context) error {
		proxy.ServeHTTP(c.Response(), c.Request())
		return nil
	})

	e.Start(":8080")
}

