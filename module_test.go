package web

import (
	"testing"
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func TestDefaultConnectionUsesConfiguredTimeouts(t *testing.T) {
	config := Config{
		Host: "127.0.0.1", Port: 8080,
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 7 * time.Second,
		WriteTimeout: 9 * time.Second, IdleTimeout: 11 * time.Second,
	}
	connection, err := (&defaultDriver{}).Connect(&Instance{Config: config})
	if err != nil {
		t.Fatalf("connect default driver: %v", err)
	}
	conn := connection.(*defaultConnect)
	if err := conn.Open(); err != nil {
		t.Fatalf("open default connection: %v", err)
	}
	if conn.server.ReadHeaderTimeout != config.ReadHeaderTimeout || conn.server.ReadTimeout != config.ReadTimeout ||
		conn.server.WriteTimeout != config.WriteTimeout || conn.server.IdleTimeout != config.IdleTimeout {
		t.Fatalf("unexpected server timeouts: %#v", conn.server)
	}
}

func TestParseConfigAliasAndDomainLists(t *testing.T) {
	cfg := parseConfig(Map{
		"alias":   []Any{"www1", "home"},
		"aliases": []Any{"cdn"},
		"domain":  []Any{"a.example.com", "b.example.com"},
		"domains": []Any{"c.example.com"},
	})

	if cfg.Alias != "www1" {
		t.Fatalf("expected first alias to be www1, got %q", cfg.Alias)
	}
	if len(cfg.Aliases) != 2 || cfg.Aliases[0] != "home" || cfg.Aliases[1] != "cdn" {
		t.Fatalf("unexpected aliases: %#v", cfg.Aliases)
	}

	if cfg.Domain != "a.example.com" {
		t.Fatalf("expected first domain to be a.example.com, got %q", cfg.Domain)
	}
	if len(cfg.Domains) != 2 || cfg.Domains[0] != "b.example.com" || cfg.Domains[1] != "c.example.com" {
		t.Fatalf("unexpected domains: %#v", cfg.Domains)
	}
}

func TestMergeConfigAllowsBoolFalseOverride(t *testing.T) {
	base := Config{
		Token:               true,
		Crypto:              true,
		HttpOnly:            true,
		AnswerDataEncode:    true,
		tokenSet:            true,
		cryptoSet:           true,
		httpOnlySet:         true,
		answerDataEncodeSet: true,
	}

	out := mergeConfig(base, parseConfig(Map{
		"token":        false,
		"crypto":       false,
		"httponly":     false,
		"answerencode": false,
	}))

	if out.Token || out.Crypto || out.HttpOnly || out.AnswerDataEncode {
		t.Fatalf("expected bools to be overridden to false, got token=%v crypto=%v httponly=%v answerencode=%v", out.Token, out.Crypto, out.HttpOnly, out.AnswerDataEncode)
	}
}

func TestParseConfigSupportsCodecAndAnswerMap(t *testing.T) {
	cfg := parseConfig(Map{
		"answerencode": true,
		"codec":        "codec_a",
	})
	if !cfg.AnswerDataEncode || cfg.AnswerDataCodec != "codec_a" {
		t.Fatalf("expected codec key to work, got encode=%v codec=%q", cfg.AnswerDataEncode, cfg.AnswerDataCodec)
	}

	cfg = parseConfig(Map{
		"answer": Map{
			"encode": false,
			"codec":  "codec_b",
		},
	})
	if cfg.AnswerDataEncode || cfg.AnswerDataCodec != "codec_b" {
		t.Fatalf("expected answer map parse, got encode=%v codec=%q", cfg.AnswerDataEncode, cfg.AnswerDataCodec)
	}
}

func TestResolveSiteByHostUsesSubdomainAliasOnly(t *testing.T) {
	m := &Module{
		siteAliases: map[string]string{
			"api":  "api",
			"gw":   "api",
			"www1": "www",
		},
	}

	if site := m.resolveSiteByHost("api.foo.com"); site != "api" {
		t.Fatalf("expected api.foo.com => api, got %q", site)
	}
	if site := m.resolveSiteByHost("gw.bar.net"); site != "api" {
		t.Fatalf("expected gw.bar.net => api, got %q", site)
	}
	if site := m.resolveSiteByHost("www1.demo.local:8090"); site != "www" {
		t.Fatalf("expected www1.demo.local:8090 => www, got %q", site)
	}
	if site := m.resolveSiteByHost("unknown.foo.com"); site != "" {
		t.Fatalf("expected unknown host to be empty, got %q", site)
	}
}

func TestSelectSiteForRequestFallsBackToDefaultSiteWhenHostUnmatched(t *testing.T) {
	m := &Module{
		defaultSite: infra.DEFAULT,
		siteAliases: map[string]string{
			"api": "api",
		},
	}

	selected, router := m.selectSiteForRequest("api.ping.*", "unknown.demo.local:8090")
	if selected != infra.DEFAULT {
		t.Fatalf("expected unmatched host to fallback to default site, got %q", selected)
	}
	if router != "ping.*" {
		t.Fatalf("expected router name ping.*, got %q", router)
	}
}

func TestSelectSiteForRequestUsesAliasWhenMatched(t *testing.T) {
	m := &Module{
		defaultSite: infra.DEFAULT,
		siteAliases: map[string]string{
			"api": "api",
		},
	}

	selected, router := m.selectSiteForRequest("api.ping.*", "api.demo.local:8090")
	if selected != "api" {
		t.Fatalf("expected matched alias host to resolve api site, got %q", selected)
	}
	if router != "ping.*" {
		t.Fatalf("expected router name ping.*, got %q", router)
	}
}

func TestRegisterSiteTypeCompatibility(t *testing.T) {
	oldOpen := module.opened
	oldConfigs := module.configs
	defer func() {
		module.opened = oldOpen
		module.configs = oldConfigs
	}()

	module.opened = false
	module.configs = map[string]Config{}

	module.Register("www", Site{})
	if _, ok := module.configs["www"]; !ok {
		t.Fatalf("expected web.Site{} registration to map into configs[\"www\"]")
	}
}

func TestUnnamedConfigRegistersConnectionDefaults(t *testing.T) {
	m := &Module{configs: map[string]Config{}}
	m.Register("", Config{Host: "127.0.0.1", Port: 9090, HttpOnly: true})

	if m.config.Host != "127.0.0.1" || m.config.Port != 9090 || !m.config.HttpOnly {
		t.Fatalf("unexpected root config: %#v", m.config)
	}
	if len(m.configs) != 0 {
		t.Fatalf("unnamed config must not create a site override: %#v", m.configs)
	}

	m.Register("api", Config{Alias: "gateway"})
	if m.configs["api"].Alias != "gateway" {
		t.Fatalf("named config must remain site-specific: %#v", m.configs)
	}
}

func TestRegisterRoutersPreservesDefaultSitePrefix(t *testing.T) {
	m := &Module{routers: map[string]Router{}}
	m.RegisterRouters(".", Routers{
		"auth.login": {Uri: "/v1/auth/login"},
	})

	if _, ok := m.routers[".auth.login"]; !ok {
		t.Fatalf("expected default-site route, got %#v", m.routers)
	}
	if _, ok := m.routers["..auth.login"]; ok {
		t.Fatalf("double-dot route must not be registered: %#v", m.routers)
	}
}

func TestConfigParsesSiteCrossOnly(t *testing.T) {
	m := &Module{
		defaultConfig: Config{},
		configs:       map[string]Config{},
		crosses:       map[string]Cross{},
	}

	m.Config(Map{
		"cross": Map{
			"allow": true,
		},
		"site": Map{
			"sys": Map{
				"name": "system",
				"cross": Map{
					"enable":  true,
					"origin":  "https://console.example.com",
					"methods": []Any{"OPTIONS", "POST"},
				},
			},
		},
	})

	if _, ok := m.crosses[infra.DEFAULT]; ok {
		t.Fatalf("expected global cross config to be ignored for web")
	}

	cross := m.crosses["sys"]
	if !cross.Allow {
		t.Fatalf("expected site.sys cross to enable allow")
	}
	if cross.Origin != "https://console.example.com" {
		t.Fatalf("expected site.sys cross origin to be parsed, got %q", cross.Origin)
	}
	if len(cross.Methods) != 2 || cross.Methods[0] != "OPTIONS" || cross.Methods[1] != "POST" {
		t.Fatalf("unexpected site.sys cross methods: %#v", cross.Methods)
	}
}
