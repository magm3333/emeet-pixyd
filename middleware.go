//go:build linux

package main

import (
	"fmt"
	"log/slog"
	"net/http"
)

type cachingFS struct {
	handler http.Handler
}

func (c cachingFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", staticCacheControl(buildVersion))
	c.handler.ServeHTTP(w, r)
}

func staticCacheControl(version string) string {
	if version == "dev" {
		return "no-store"
	}

	return fmt.Sprintf("public, max-age=%d", int64(staticCacheMaxAge.Seconds()))
}

//nolint:gochecknoglobals
var loggingMiddleware = loggingMiddlewareFactory(slog.Default())

//nolint:gochecknoglobals
var securityHeaderMiddleware = securityHeadersMiddleware

//nolint:gochecknoglobals
var requestIDMW = requestIDMiddleware
