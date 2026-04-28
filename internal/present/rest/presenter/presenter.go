package presenter

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

type errorResponse struct {
	Error string `json:"error"`
}

// OK wraps a successful response.
func OK(c echo.Context, payload any) error {
	return c.JSON(http.StatusOK, payload)
}

func BadRequest(c echo.Context, err error) error {
	fmt.Println("Bad request:", err)
	return c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
}

func BadRequestMessage(c echo.Context, msg string) error {
	fmt.Println("Bad request:", msg)
	return c.JSON(http.StatusBadRequest, errorResponse{Error: msg})
}

func NotFound(c echo.Context, msg string) error {
	fmt.Println("Not found:", msg)
	return c.JSON(http.StatusNotFound, errorResponse{Error: msg})
}

func InternalError(c echo.Context, err error) error {
	fmt.Println("Internal error:", err)
	return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
}

func Forbidden(c echo.Context, msg string) error {
	fmt.Println("Forbidden:", msg)
	return c.JSON(http.StatusForbidden, errorResponse{Error: msg})
}

func Redirect(c echo.Context, location string) error {
	fmt.Println("Redirecting to:", location)
	return c.Redirect(http.StatusFound, location)
}
