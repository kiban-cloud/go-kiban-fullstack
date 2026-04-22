package middleware

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/commons"
	"github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/appContext"
	http_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/http/errors"
	"github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/logger"

	"github.com/gin-gonic/gin"
)

const (
	TRACE_HEADER      = "X-Cloud-Trace-Context"
	REQUEST_ID_HEADER = "X-Request-ID"
)

type LoggerMiddleware struct{}

func NewLoggerMiddleware() *LoggerMiddleware {
	return &LoggerMiddleware{}
}

type responseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func (m *LoggerMiddleware) Middleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()
		requestID := commons.NewUUID()
		ctx.Header(REQUEST_ID_HEADER, requestID)

		traceHeader := ctx.GetHeader(TRACE_HEADER)
		traceID := logger.ParseTraceContext(traceHeader)

		newCtx := appContext.WithRequestAndTraceID(ctx.Request.Context(), requestID, traceID)
		ctx.Request = ctx.Request.WithContext(newCtx)

		var requestBody []byte
		if ctx.Request.Body != nil {
			var err error
			requestBody, err = io.ReadAll(ctx.Request.Body)
			if err != nil {
				log.Printf("[ERROR] Failed to read request body: %v", err)
			} else {
				ctx.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
			}
		}

		defer func() {
			if err := recover(); err != nil {
				errCtx := logger.RequestErrorInfo{
					Method:      ctx.Request.Method,
					Path:        ctx.Request.URL.Path,
					Query:       ctx.Request.URL.RawQuery,
					IP:          ctx.ClientIP(),
					UserAgent:   ctx.Request.UserAgent(),
					StatusCode:  http.StatusInternalServerError,
					Duration:    time.Since(start),
					Headers:     ctx.Request.Header,
					RequestBody: string(requestBody),
				}

				if errError, ok := err.(error); ok {
					errCtx.Error = errError
				} else {
					errCtx.Error = fmt.Errorf("panic")
				}

				logger.LogHTTPError(ctx, ctx.Request.Context(), errCtx, true)

				if !ctx.Writer.Written() {
					ctx.JSON(http.StatusInternalServerError,
						http_errors.NewInternalServerErrorResponse(ctx, errCtx.Error, "panic"))
				}

				ctx.Abort()
			}
		}()

		responseBody := &bytes.Buffer{}
		writer := &responseWriter{
			ResponseWriter: ctx.Writer,
			body:           responseBody,
		}
		ctx.Writer = writer

		ctx.Next()

		statusCode := ctx.Writer.Status()
		errorInContext, ok := ctx.Get(http_errors.ERROR_MESSAGE_CONTEXT_KEY)
		var errorInContextError error
		if ok {
			errorInContextError = errorInContext.(error)
		}

		if statusCode >= 400 || errorInContextError != nil {
			errCtx := logger.RequestErrorInfo{
				Method:       ctx.Request.Method,
				Path:         ctx.Request.URL.Path,
				Query:        ctx.Request.URL.RawQuery,
				IP:           ctx.ClientIP(),
				UserAgent:   ctx.Request.UserAgent(),
				StatusCode:   statusCode,
				Duration:     time.Since(start),
				Headers:      ctx.Request.Header,
				RequestBody:  string(requestBody),
				ResponseBody: responseBody.String(),
				Error:        errorInContextError,
			}

			logger.LogHTTPError(ctx, ctx.Request.Context(), errCtx, false)
		}
	}
}
