package middleware

import (
	"net/http"
	"strings"

	http_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/errors"

	"github.com/gin-gonic/gin"
	errorsWrapper "github.com/pkg/errors"
)

const contextRequestValidatedKey = "validated_request"

func BindMiddleware[T any]() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req T

		if strings.HasPrefix(ctx.GetHeader("Content-Type"), "application/json") {
			if err := ctx.ShouldBindJSON(&req); err != nil {
				fieldErrors := http_errors.ExtractValidationErrors(err)
				if len(fieldErrors) > 0 {
					statusCode, errorResponse := http_errors.NewValidationErrorResponse(fieldErrors)
					ctx.JSON(statusCode, errorResponse)
					ctx.Abort()
					return
				}
				ctx.JSON(http.StatusBadRequest, http_errors.NewBadRequestErrorResponse(ctx, err, "error binding request"))
				ctx.Abort()
				return
			}
		} else {
			if err := ctx.ShouldBind(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, http_errors.NewBadRequestErrorResponse(ctx, err, "error binding request"))
				ctx.Abort()
				return
			}
		}

		ctx.Set(contextRequestValidatedKey, req)
		ctx.Next()
	}
}

func GetValidatedRequest[T any](ctx *gin.Context) T {
	req, ok := ctx.Get(contextRequestValidatedKey)
	if !ok {
		panic(errorsWrapper.New("request not found in context"))
	}

	typed := req.(T)
	return typed
}
