package http_errors

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

const ERROR_MESSAGE_CONTEXT_KEY = "ERROR_MESSAGE"

const (
	ERROR_CODE_INVALID_INPUT          = "INVALID_INPUT"
	ERROR_CODE_INTERNAL_SERVER_ERROR  = "INTERNAL_SERVER_ERROR"
	ERROR_CODE_UNAUTHORIZED           = "UNAUTHORIZED"
)

type ErrorResponse struct {
	Message string        `json:"message"`
	Code    string        `json:"code,omitempty"`
	Details *ErrorDetails `json:"details,omitempty"`
}

type ErrorDetails struct {
	FieldErrors []FieldError `json:"fieldErrors,omitempty"`
}

type FieldError struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}

func ExtractValidationErrors(err error) []FieldError {
	var fieldErrors []FieldError

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, fieldError := range validationErrors {
			field := buildFieldPath(fieldError)
			issue := getValidationIssue(fieldError)
			fieldErrors = append(fieldErrors, FieldError{
				Field: field,
				Issue: issue,
			})
		}
	}

	return fieldErrors
}

func buildFieldPath(err validator.FieldError) string {
	namespace := err.Namespace()

	parts := strings.Split(namespace, ".")
	if len(parts) > 1 {
		parts = parts[1:]
	}

	var fieldPath strings.Builder
	for i, part := range parts {
		if idx := strings.Index(part, "["); idx != -1 {
			fieldName := part[:idx]
			arrayIndex := part[idx:]

			if i > 0 {
				fieldPath.WriteString(".")
			}
			fieldPath.WriteString(toCamelCase(fieldName))
			fieldPath.WriteString(arrayIndex)
		} else {
			if i > 0 {
				fieldPath.WriteString(".")
			}
			fieldPath.WriteString(toCamelCase(part))
		}
	}

	return fieldPath.String()
}

func toCamelCase(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func getValidationIssue(fieldError validator.FieldError) string {
	switch fieldError.Tag() {
	case "required":
		return "campo requerido"
	case "min":
		return fmt.Sprintf("debe tener al menos %s caracteres", fieldError.Param())
	case "max":
		return fmt.Sprintf("debe tener máximo %s caracteres", fieldError.Param())
	case "email":
		return "formato de email inválido"
	case "mongodb":
		return "formato inválido"
	case "len":
		return fmt.Sprintf("debe tener exactamente %s caracteres", fieldError.Param())
	case "uuid":
		return "debe ser un UUID válido"
	case "e164":
		return "debe ser un número de teléfono válido en formato E.164"
	case "startswith":
		return fmt.Sprintf("debe comenzar con '%s'", fieldError.Param())
	case "gte":
		return fmt.Sprintf("debe ser mayor o igual a %s", fieldError.Param())
	case "lte":
		return fmt.Sprintf("debe ser menor o igual a %s", fieldError.Param())
	case "gt":
		return fmt.Sprintf("debe ser mayor a %s", fieldError.Param())
	case "lt":
		return fmt.Sprintf("debe ser menor a %s", fieldError.Param())
	case "xmlExtension":
		return "archivo con extensión inválida, debe ser un archivo XML (.xml)"
	case "xmlContent":
		return "archivo con contenido inválido, no corresponde a un archivo XML válido"
	case "maxFileSize":
		return "archivo con tamaño máximo excedido, debe ser menor a 10MB"
	case "dive":
		return "error en validación de array"
	default:
		return fmt.Sprintf("validación fallida: %s", fieldError.Tag())
	}
}

func NewInternalServerErrorResponse(c *gin.Context, err error, message string) ErrorResponse {
	LogInternalServerErrorResponse(c, err, message)

	return ErrorResponse{
		Message: message,
		Code:    ERROR_CODE_INTERNAL_SERVER_ERROR,
	}
}

func NewUnauthorizedErrorResponse(c *gin.Context, err error, message string) ErrorResponse {
	LogInternalServerErrorResponse(c, err, message)

	return ErrorResponse{
		Message: message,
		Code:    ERROR_CODE_UNAUTHORIZED,
	}
}

func NewBadRequestErrorResponse(c *gin.Context, err error, message string) ErrorResponse {
	LogInternalServerErrorResponse(c, err, message)

	return ErrorResponse{
		Message: message,
		Code:    ERROR_CODE_INVALID_INPUT,
	}
}

func NewValidationErrorResponse(fieldErrors []FieldError) (int, ErrorResponse) {
	return http.StatusBadRequest, ErrorResponse{
		Message: "invalid input",
		Code:    ERROR_CODE_INVALID_INPUT,
		Details: &ErrorDetails{
			FieldErrors: fieldErrors,
		},
	}
}

func LogInternalServerErrorResponse(c *gin.Context, err error, message string) {
	c.Set(ERROR_MESSAGE_CONTEXT_KEY, fmt.Errorf("internal server error; %s; %w", message, err))
}
