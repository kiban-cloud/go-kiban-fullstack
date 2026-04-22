package appContext

import (
	"context"

	infrastructure_common "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/common"

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
	if traceID != "" {
		ctx = context.WithValue(ctx, infrastructure_common.TRACE_KEY, traceID)
	}
	ctx = context.WithValue(ctx, infrastructure_common.REQUEST_ID, requestID)
	return ctx
}
