package httpx

import (
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// ── Responses ─────────────────────────────────────────────────

func OK(c echo.Context, data interface{}) error      { return c.JSON(http.StatusOK, data) }
func Created(c echo.Context, data interface{}) error { return c.JSON(http.StatusCreated, data) }
func NoContent(c echo.Context) error                 { return c.NoContent(http.StatusNoContent) }

func Error(c echo.Context, status int, code, msg string) error {
	return c.JSON(status, map[string]string{"error_code": code, "message": msg})
}
func BadRequest(c echo.Context, msg string) error {
	return Error(c, http.StatusBadRequest, "VALIDATION_ERROR", msg)
}
func NotFound(c echo.Context, resource string) error {
	return Error(c, http.StatusNotFound, "NOT_FOUND", resource+" not found")
}
func Internal(c echo.Context) error {
	return Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Something went wrong")
}
func Forbidden(c echo.Context, msg string) error {
	return Error(c, http.StatusForbidden, "FORBIDDEN", msg)
}
func Conflict(c echo.Context, code, msg string) error {
	return Error(c, http.StatusConflict, code, msg)
}

// ── Validator ─────────────────────────────────────────────────

type Validator struct{ v *validator.Validate }

func NewValidator() *Validator { return &Validator{v: validator.New()} }
func (vl *Validator) Validate(i interface{}) error { return vl.v.Struct(i) }

func BindAndValidate(c echo.Context, dst interface{}) error {
	if err := c.Bind(dst); err != nil {
		return BadRequest(c, "Invalid request body")
	}
	if err := c.Validate(dst); err != nil {
		return BadRequest(c, err.Error())
	}
	return nil
}
