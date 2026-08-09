package web

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func TestPreprocessingLimitsRequestBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://example.test/upload", strings.NewReader("12345"))
	site := &webSite{Config: Config{MaxBodyBytes: 4}}
	ctx := site.newContext()
	ctx.reader = request
	ctx.writer = recorder

	site.preprocessing(ctx)
	_, err := io.ReadAll(request.Body)
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected MaxBytesError, got %v", err)
	}
}

var prepareLoadInfraOnce sync.Once

func prepareLoadInfra(t *testing.T) {
	t.Helper()
	prepareLoadInfraOnce.Do(func() {
		oldArgs := os.Args
		os.Args = []string{oldArgs[0]}
		defer func() { os.Args = oldArgs }()
		infra.Prepare()
	})
}

func TestContainsOriginStrictMatch(t *testing.T) {
	if containsOrigin([]string{"https://good.com"}, "https://good.com.evil.com") {
		t.Fatalf("expected strict origin check to reject prefix match")
	}

	if !containsOrigin([]string{"https://good.com"}, "https://good.com") {
		t.Fatalf("expected exact origin to pass")
	}
}

func TestContainsOriginWildcard(t *testing.T) {
	if !containsOrigin([]string{"https://*.good.com"}, "https://api.good.com") {
		t.Fatalf("expected wildcard subdomain to pass")
	}

	if containsOrigin([]string{"https://*.good.com"}, "https://good.com") {
		t.Fatalf("expected bare root domain to fail wildcard subdomain match")
	}
}

func TestCrossingHandlesOptionsWithoutRoute(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "http://sys.example.com/launch", nil)
	req.Header.Set("Origin", "https://console.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	site := &webSite{Config: Config{}, Cross: Cross{Allow: true}}
	ctx := &Context{
		Meta:    infra.NewMeta(),
		site:    site,
		reader:  req,
		writer:  rec,
		output:  wrapResponseWriter(rec),
		headers: map[string]string{},
		cookies: map[string]Cookie{},
		Method:  http.MethodOptions,
		Path:    "/launch",
		Uri:     "/launch",
	}

	site.serve(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected preflight without route to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://console.example.com" {
		t.Fatalf("expected allow origin header, got %q", got)
	}
}

func TestExpandRouterMergesLoadFromMethodRouting(t *testing.T) {
	routers := expandRouter("demo", Router{
		Uri: "/demo",
		Loading: Loading{
			"staff": {Invoke: "sys.staff.get"},
		},
		Routing: Routing{
			POST: Router{
				Loading: Loading{
					"role": {Invoke: "sys.role.get"},
				},
			},
		},
	})

	router, ok := routers["demo.POST"]
	if !ok {
		router, ok = routers["demo.post"]
	}
	if !ok {
		t.Fatalf("expected expanded POST router")
	}
	if len(router.Loading) != 2 {
		t.Fatalf("expected merged loading configs, got %#v", router.Loading)
	}
	if router.Loading["staff"].Invoke != "sys.staff.get" {
		t.Fatalf("expected inherited staff loader, got %#v", router.Loading["staff"])
	}
	if router.Loading["role"].Invoke != "sys.role.get" {
		t.Fatalf("expected method role loader, got %#v", router.Loading["role"])
	}
}

func TestLoadingInvokesAndStoresLocals(t *testing.T) {
	infra.Register("web.load.test", infra.Method{
		Action: func(ctx *infra.Context) Map {
			return Map{"id": ctx.Args["id"], "name": "demo"}
		},
	})
	prepareLoadInfra(t)

	site := &webSite{}
	ctx := &Context{
		Meta:   infra.NewMeta(),
		Config: Router{Loading: Loading{"staff": {Invoke: "web.load.test", Value: "staff_id", Args: "id", Required: true}}},
		Args:   Map{"staff_id": 7},
		Value:  Map{},
		Locals: Map{},
	}

	site.loading(ctx)

	staff, ok := ctx.Locals["staff"].(Map)
	if !ok {
		t.Fatalf("expected staff local map, got %T %#v", ctx.Locals["staff"], ctx.Locals["staff"])
	}
	if id, ok := staff["id"].(int); !ok || id != 7 {
		t.Fatalf("expected invoked id=7, got %T %#v", staff["id"], staff["id"])
	}
	if name, ok := staff["name"].(string); !ok || name != "demo" {
		t.Fatalf("expected invoked name demo, got %T %#v", staff["name"], staff["name"])
	}
}

func TestLoadingFailsWhenRequiredValueMissing(t *testing.T) {
	site := &webSite{}
	ctx := &Context{
		Meta:   infra.NewMeta(),
		Config: Router{Loading: Loading{"staff": {Invoke: "web.load.test", Value: "staff_id", Required: true}}},
		Args:   Map{},
		Value:  Map{},
		Locals: Map{},
	}

	site.loading(ctx)

	if res := ctx.Result(); res == nil || res.OK() {
		t.Fatalf("expected required loader to fail, got %#v", res)
	}
	if ctx.handling != "failed" {
		t.Fatalf("expected failed handling, got %q", ctx.handling)
	}
}
