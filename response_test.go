package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func TestBodyPreservesExplicitCookieAttributes(t *testing.T) {
	rec := httptest.NewRecorder()
	site := &webSite{Config: Config{Domain: "example.com", HttpOnly: true, MaxAge: time.Hour}}
	ctx := &Context{
		Meta: infra.NewMeta(), site: site, writer: rec,
		headers: map[string]string{}, cookies: map[string]Cookie{}, Code: StatusNoContent,
	}
	ctx.Cookie("csrf", http.Cookie{
		Name: "csrf", Value: "csrf-value", Path: "/auth", HttpOnly: false,
		Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: 300,
	})
	ctx.Cookie("access", http.Cookie{
		Name: "access", Value: "access-value", Path: "/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	ctx.Cookie("default", "default-value")

	site.body(ctx)

	cookies := map[string]*http.Cookie{}
	for _, cookie := range rec.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
	csrf := cookies["csrf"]
	if csrf == nil || csrf.HttpOnly || !csrf.Secure || csrf.Path != "/auth" || csrf.SameSite != http.SameSiteStrictMode || csrf.MaxAge != 300 || csrf.Domain != "" {
		t.Fatalf("explicit CSRF cookie attributes were changed: %#v", csrf)
	}
	access := cookies["access"]
	if access == nil || !access.HttpOnly || !access.Secure || access.SameSite != http.SameSiteLaxMode || access.MaxAge != 600 {
		t.Fatalf("explicit access cookie attributes were changed: %#v", access)
	}
	defaults := cookies["default"]
	if defaults == nil || !defaults.HttpOnly || defaults.Path != "/" || defaults.Domain != "example.com" || defaults.MaxAge != int(time.Hour.Seconds()) {
		t.Fatalf("string cookie did not use site defaults: %#v", defaults)
	}
}

func TestBodyAnswerEncodesDataBySiteConfig(t *testing.T) {
	infra.Register("answer_test_web", infra.Codec{
		Encode: func(v Any) (Any, error) {
			return "encoded-web", nil
		},
		Decode: func(d Any, v Any) (Any, error) {
			return d, nil
		},
	})

	if got, err := infra.Encrypt("answer_test_web", Map{"name": "demo"}); err != nil || got != "encoded-web" {
		t.Fatalf("expected codec to work, got=%v err=%v", got, err)
	}

	rec := httptest.NewRecorder()
	site := &webSite{Config: Config{AnswerDataEncode: true, AnswerDataCodec: "answer_test_web"}}
	ctx := &Context{
		Meta:    infra.NewMeta(),
		site:    site,
		writer:  rec,
		headers: map[string]string{},
		cookies: map[string]Cookie{},
		Config:  Router{},
		Code:    StatusOK,
	}

	site.bodyAnswer(ctx, httpAnswerBody{
		code: 0,
		text: "",
		data: Map{"name": "demo"},
	})

	var payload map[string]Any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON payload, got err=%v body=%s", err, rec.Body.String())
	}

	data, ok := payload["data"].(string)
	if !ok {
		t.Fatalf("expected encoded data as string, got %T (%#v)", payload["data"], payload["data"])
	}
	if data != "encoded-web" {
		t.Fatalf("expected encoded data %q, got %q", "encoded-web", data)
	}
}

func TestBodyProxyJoinsTargetPathAndSetsForwardedHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]Any{
			"path":              r.URL.Path,
			"query":             r.URL.RawQuery,
			"host":              r.Host,
			"x_forwarded_for":   r.Header.Get("X-Forwarded-For"),
			"x_forwarded_host":  r.Header.Get("X-Forwarded-Host"),
			"x_forwarded_proto": r.Header.Get("X-Forwarded-Proto"),
		})
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "http://frontend.demo.local/api/items?id=9", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	rec := httptest.NewRecorder()

	ctx := &Context{
		Meta:    infra.NewMeta(),
		site:    &webSite{Config: Config{}},
		reader:  req,
		writer:  rec,
		headers: map[string]string{},
		cookies: map[string]Cookie{},
	}
	ctx.Proxy(upstream.URL + "/base?fixed=1")

	site := &webSite{}
	site.body(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected proxy status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]Any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON payload, got err=%v body=%s", err, rec.Body.String())
	}

	if got := payload["path"]; got != "/base/api/items" {
		t.Fatalf("expected joined path /base/api/items, got %#v", got)
	}
	if got := payload["query"]; got != "fixed=1&id=9" {
		t.Fatalf("expected merged query fixed=1&id=9, got %#v", got)
	}
	if got := payload["x_forwarded_host"]; got != "frontend.demo.local" {
		t.Fatalf("expected forwarded host frontend.demo.local, got %#v", got)
	}
	if got := payload["x_forwarded_proto"]; got != "http" {
		t.Fatalf("expected forwarded proto http, got %#v", got)
	}
	if got := payload["x_forwarded_for"]; got != "10.0.0.1, 192.0.2.1" {
		t.Fatalf("expected appended forwarded for, got %#v", got)
	}
}
