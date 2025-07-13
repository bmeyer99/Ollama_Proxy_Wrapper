package main

import (
	"context"
	"net/http"
	"time"
)

// ProxyContext stores request context for metrics collection
type ProxyContext struct {
	StartTime        time.Time
	Model            string
	Prompt           string
	Endpoint         string
	PromptCategory   string
	Writer           http.ResponseWriter
	Request          *http.Request
	PromptTokens     int
	LoadDuration     float64
	TotalDuration    float64
	ResponsePreview  string
	TimeToFirstToken float64
	ClientIP         string
}

type contextKey string

const proxyContextKey contextKey = "proxy-context"

// withProxyContext adds ProxyContext to the request context
func withProxyContext(ctx context.Context, pctx *ProxyContext) context.Context {
	return context.WithValue(ctx, proxyContextKey, pctx)
}

// getProxyContext retrieves ProxyContext from the request context
func getProxyContext(ctx context.Context) *ProxyContext {
	if pctx, ok := ctx.Value(proxyContextKey).(*ProxyContext); ok {
		return pctx
	}
	return nil
}