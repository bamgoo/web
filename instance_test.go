package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/infrago/base"
)

func TestServeFilterCanShortCircuitWithResponse(t *testing.T) {
	site := &webSite{Config: Config{}}
	site.serveFilters = []ctxFunc{func(ctx *Context) {
		ctx.JSON(Map{"error": "rejected"}, http.StatusForbidden)
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/protected", nil)
	ctx := site.newContext()
	ctx.reader = request
	ctx.writer = wrapResponseWriter(recorder)
	ctx.output = ctx.writer.(*responseWriter)

	site.open(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"error":"rejected"`) {
		t.Fatalf("expected rejection body, got %q", body)
	}
}
