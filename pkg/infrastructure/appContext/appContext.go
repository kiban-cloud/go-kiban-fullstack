package appContext

import (
	"context"

	"github.com/kiban-cloud/go-kiban-fullstack/logger"

	"github.com/gin-gonic/gin"
)

type RequestContext struct {
	context.Context
}

func FromGin(c *gin.Context) RequestContext {
	return RequestContext{Context: c.Request.Context()}
}

func FromContextForTestingPurposes(ctx context.Context) RequestContext {
	return RequestContext{Context: ctx}
}

func WithRequestAndTraceID(ctx context.Context, requestID, traceID string) context.Context {
	return logger.WithRequestAndTrace(ctx, requestID, traceID)
}
