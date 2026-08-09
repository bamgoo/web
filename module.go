package web

import (
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func init() {
	infra.Mount(module)
}

var module = &Module{
	defaultConfig: Config{Driver: DEFAULT, Charset: UTF8, Port: 8080},
	drivers:       make(map[string]Driver),
	configs:       make(map[string]Config),
	crosses:       make(map[string]Cross),
	routers:       make(map[string]Router),
	filters:       make(map[string]Filter),
	handlers:      make(map[string]Handler),
	sites:         make(map[string]*webSite),
	siteAliases:   make(map[string]string),
	defaultSite:   infra.DEFAULT,
}

func SetFS(fsys fs.FS) {
	infra.AssetFS(fsys)
}

type (
	Module struct {
		mutex sync.Mutex

		opened  bool
		started bool

		defaultConfig Config

		drivers map[string]Driver
		config  Config
		configs map[string]Config
		crosses map[string]Cross

		routers  map[string]Router
		filters  map[string]Filter
		handlers map[string]Handler

		sites       map[string]*webSite
		siteAliases map[string]string
		defaultSite string

		instance *Instance
	}

	Config struct {
		Driver            string
		Port              int
		Host              string
		ReadHeaderTimeout time.Duration
		ReadTimeout       time.Duration
		WriteTimeout      time.Duration
		IdleTimeout       time.Duration
		ShutdownTimeout   time.Duration
		MaxBodyBytes      int64

		CertFile string
		KeyFile  string

		Charset string

		Cookie   string
		Token    bool
		Expire   time.Duration
		Crypto   bool
		MaxAge   time.Duration
		HttpOnly bool
		// AnswerDataEncode toggles ctx.Answer(data) payload encoding.
		AnswerDataEncode bool
		// AnswerDataCodec is codec name used by infra.Mapping Var.Encode.
		AnswerDataCodec string

		Upload   string
		Static   string
		Shared   string
		Defaults []string

		Domain  string
		Domains []string
		Alias   string
		Aliases []string

		Setting Map

		tokenSet            bool
		cryptoSet           bool
		httpOnlySet         bool
		answerDataEncodeSet bool
	}

	Configs map[string]Config
	Site    Config
	Sites   map[string]Site

	Cross struct {
		Allow   bool
		Method  string
		Methods []string
		Origin  string
		Origins []string
		Header  string
		Headers []string
	}

	Instance struct {
		connect  Connection
		Config   Config
		Setting  Map
		Delegate Delegate
	}

	webSite struct {
		Name    string
		Config  Config
		Cross   Cross
		Setting Map
		Hosts   []string
		Aliases []string

		routers  map[string]Router
		filters  map[string]Filter
		handlers map[string]Handler

		routerInfos map[string]Info
		routerOrder []string

		serveFilters    []ctxFunc
		requestFilters  []ctxFunc
		executeFilters  []ctxFunc
		responseFilters []ctxFunc

		notFoundHandlers []ctxFunc
		errorHandlers    []ctxFunc
		failedHandlers   []ctxFunc
		unsignedHandlers []ctxFunc
		unauthedHandlers []ctxFunc
		deniedHandlers   []ctxFunc
	}
)

// Register dispatches registrations.
func (m *Module) Register(name string, value Any) {
	switch v := value.(type) {
	case Driver:
		m.RegisterDriver(name, v)
	case Config:
		if name == "" {
			m.RegisterRootConfig(v)
		} else {
			m.RegisterConfig(name, v)
		}
	case Configs:
		m.RegisterConfigs(v)
	case Site:
		m.RegisterConfig(name, Config(v))
	case Sites:
		for siteName, site := range v {
			m.RegisterConfig(siteName, Config(site))
		}
	case Router:
		m.RegisterRouter(name, v)
	case Routers:
		m.RegisterRouters(name, v)
	case Filter:
		m.RegisterFilter(name, v)
	case Handler:
		m.RegisterHandler(name, v)
	}
}

// RegisterRootConfig registers programmatic connection defaults shared by all
// sites. Named Config registrations remain site-specific.
func (m *Module) RegisterRootConfig(config Config) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}
	m.config = mergeConfig(m.config, config)
}

// RegisterRouters registers multiple routers.
func (m *Module) RegisterRouters(prefix string, routers Routers) {
	for name, router := range routers {
		target := name
		if prefix != "" {
			target = prefix + "." + name
		}
		m.RegisterRouter(target, router)
	}
}

// RegisterDriver registers a web driver.
func (m *Module) RegisterDriver(name string, driver Driver) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if driver == nil {
		panic("Invalid web driver: " + name)
	}
	if name == "" {
		name = DEFAULT
	}

	if infra.Override() {
		m.drivers[name] = driver
	} else if _, ok := m.drivers[name]; !ok {
		m.drivers[name] = driver
	}
}

// RegisterConfig registers web config for a named site.
func (m *Module) RegisterConfig(name string, config Config) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	if name == "" {
		name = infra.DEFAULT
	}

	name = strings.ToLower(name)
	if infra.Override() {
		m.configs[name] = config
	} else if _, ok := m.configs[name]; !ok {
		m.configs[name] = config
	}
}

// RegisterConfigs registers multiple configs.
func (m *Module) RegisterConfigs(configs Configs) {
	for name, cfg := range configs {
		m.RegisterConfig(name, cfg)
	}
}

// Config parses global config for web.
func (m *Module) Config(global Map) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	if cfgAny, ok := global["web"]; ok {
		if cfgMap, ok := cfgAny.(Map); ok && cfgMap != nil {
			root := Map{}
			for key, val := range cfgMap {
				if key == "site" || key == "sites" {
					if siteMap, ok := crossMapValue(val); ok {
						siteRoot := Map{}
						for siteName, siteVal := range siteMap {
							if conf, ok := crossMapValue(siteVal); ok {
								if siteName == "cross" {
									m.configureSiteCross(infra.DEFAULT, conf)
									continue
								}
								m.configureSite(siteName, conf)
							} else {
								siteRoot[siteName] = siteVal
							}
						}
						if len(siteRoot) > 0 {
							m.configureSite(infra.DEFAULT, siteRoot)
						}
					}
					continue
				}
				root[key] = val
			}
			m.configureRoot(root)
		}
	}

	if siteAny, ok := global["site"]; ok {
		if siteMap, ok := crossMapValue(siteAny); ok && siteMap != nil {
			root := Map{}
			for key, val := range siteMap {
				if conf, ok := crossMapValue(val); ok {
					if key == "cross" {
						m.configureSiteCross(infra.DEFAULT, conf)
						continue
					}
					m.configureSite(key, conf)
				} else {
					root[key] = val
				}
			}
			if len(root) > 0 {
				m.configureSite(infra.DEFAULT, root)
			}
		}
	}

}

func (m *Module) configureRoot(conf Map) {
	cfg := mergeConfig(m.defaultConfig, m.config)
	cfg = mergeConfig(cfg, parseConfig(conf))
	m.config = cfg
}

func (m *Module) configureSite(name string, conf Map) {
	name = strings.ToLower(name)
	if crossConf, ok := crossMapValue(conf["cross"]); ok && crossConf != nil {
		m.configureSiteCross(name, crossConf)
	}
	cfg := mergeConfig(mergeConfig(m.defaultConfig, m.config), m.configs[name])
	cfg = mergeConfig(cfg, parseConfig(conf))
	m.configs[name] = cfg
}

func (m *Module) configureSiteCross(name string, conf Map) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = infra.DEFAULT
	}
	if m.crosses == nil {
		m.crosses = make(map[string]Cross)
	}
	cross := mergeCross(m.crosses[name], conf)
	m.crosses[name] = cross
}

// Setup initializes defaults and sites.
func (m *Module) Setup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	m.config = mergeConfig(m.defaultConfig, m.config)
	m.applyDefaults(&m.config)

	names := map[string]struct{}{infra.DEFAULT: {}}
	for name := range m.configs {
		names[name] = struct{}{}
	}
	for key := range m.routers {
		siteName, _ := splitPrefix(key)
		if siteName != "*" {
			names[siteName] = struct{}{}
		}
	}
	for key := range m.filters {
		siteName, _ := splitPrefix(key)
		if siteName != "*" {
			names[siteName] = struct{}{}
		}
	}
	for key := range m.handlers {
		siteName, _ := splitPrefix(key)
		if siteName != "*" {
			names[siteName] = struct{}{}
		}
	}

	m.sites = make(map[string]*webSite, len(names))
	m.siteAliases = make(map[string]string, len(names)*2)
	m.defaultSite = infra.DEFAULT

	for name := range names {
		baseCfg := mergeConfig(m.defaultConfig, m.config)
		if cfg, ok := m.configs[name]; ok {
			baseCfg = mergeConfig(baseCfg, cfg)
		}
		m.applyDefaults(&baseCfg)
		m.applySiteDefaults(name, &baseCfg)

		site := &webSite{
			Name:     name,
			Config:   baseCfg,
			Cross:    m.crosses[name],
			Setting:  baseCfg.Setting,
			routers:  make(map[string]Router),
			filters:  make(map[string]Filter),
			handlers: make(map[string]Handler),
		}
		site.Hosts = m.resolveSiteHosts(name, &site.Config)
		site.Aliases = m.resolveSiteAliases(name, &site.Config)
		m.sites[name] = site
	}

	if _, ok := m.sites[infra.DEFAULT]; !ok {
		for name := range m.sites {
			m.defaultSite = name
			break
		}
	}

	for key, router := range m.routers {
		siteName, routerName := splitPrefix(key)
		if siteName == "*" {
			for _, site := range m.sites {
				applyRouter(site, routerName, router)
			}
			continue
		}
		if site, ok := m.sites[siteName]; ok {
			applyRouter(site, routerName, router)
		}
	}
	for key, filter := range m.filters {
		siteName, filterName := splitPrefix(key)
		if siteName == "*" {
			for _, site := range m.sites {
				storeFilter(site.filters, filterName, filter)
			}
			continue
		}
		if site, ok := m.sites[siteName]; ok {
			storeFilter(site.filters, filterName, filter)
		}
	}
	for key, handler := range m.handlers {
		siteName, handlerName := splitPrefix(key)
		if siteName == "*" {
			for _, site := range m.sites {
				storeHandler(site.handlers, handlerName, handler)
			}
			continue
		}
		if site, ok := m.sites[siteName]; ok {
			storeHandler(site.handlers, handlerName, handler)
		}
	}

	for _, site := range m.sites {
		for _, alias := range site.Aliases {
			alias = normalizeAlias(alias)
			if alias == "" {
				continue
			}
			if infra.Override() {
				m.siteAliases[alias] = site.Name
			} else if _, ok := m.siteAliases[alias]; !ok {
				m.siteAliases[alias] = site.Name
			}
		}
		m.buildSite(site)
	}
}

func (m *Module) applyDefaults(cfg *Config) {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = 8080
	}
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Charset == "" {
		cfg.Charset = UTF8
	}
	if cfg.Defaults == nil || len(cfg.Defaults) == 0 {
		cfg.Defaults = []string{"index.html", "default.html", "index.htm", "default.htm"}
	}
	if cfg.Upload == "" {
		cfg.Upload = os.TempDir()
	}
	if cfg.Static == "" {
		cfg.Static = "asset/statics"
	}
	if cfg.Shared == "" {
		cfg.Shared = "shared"
	}
	if cfg.Expire == 0 {
		cfg.Expire = time.Hour * 24 * 30
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = time.Hour * 24 * 30
	}
}

func (m *Module) applySiteDefaults(name string, cfg *Config) {
	if cfg.Upload == "" {
		cfg.Upload = m.config.Upload
	}
	if cfg.Static == "" {
		cfg.Static = m.config.Static
	}
	// Site-level static defaults to a site folder under global static root.
	if m.config.Static != "" && cfg.Static == m.config.Static && name != infra.DEFAULT {
		cfg.Static = path.Join(m.config.Static, name)
	}
	if len(cfg.Defaults) == 0 {
		cfg.Defaults = m.config.Defaults
	}
	if cfg.Shared == "" {
		cfg.Shared = m.config.Shared
	}
}

func (m *Module) buildSite(site *webSite) {
	site.routerInfos = make(map[string]Info)
	keys := make([]string, 0, len(site.routers))
	for key := range site.routers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		router := site.routers[key]
		for i, uri := range router.Uris {
			infoKey := key
			if i > 0 {
				infoKey = key + "." + strconv.Itoa(i)
			}
			site.routerInfos[infoKey] = Info{
				Method: router.method,
				Uri:    uri,
				Router: key,
				Entry:  router.Key,
				Args:   router.Args,
			}
		}
	}
	site.routerOrder = make([]string, 0, len(site.routerInfos))
	for key := range site.routerInfos {
		site.routerOrder = append(site.routerOrder, key)
	}
	sort.SliceStable(site.routerOrder, func(i, j int) bool {
		left := site.routerInfos[site.routerOrder[i]]
		right := site.routerInfos[site.routerOrder[j]]

		if left.Uri != right.Uri {
			return left.Uri < right.Uri
		}

		leftWeight := 1
		rightWeight := 1
		if left.Method != "" {
			leftWeight = 0
		}
		if right.Method != "" {
			rightWeight = 0
		}
		if leftWeight != rightWeight {
			return leftWeight < rightWeight
		}
		if left.Method != right.Method {
			return left.Method < right.Method
		}

		return site.routerOrder[i] < site.routerOrder[j]
	})

	site.serveFilters = make([]ctxFunc, 0, len(site.filters))
	site.requestFilters = make([]ctxFunc, 0, len(site.filters))
	site.executeFilters = make([]ctxFunc, 0, len(site.filters))
	site.responseFilters = make([]ctxFunc, 0, len(site.filters))
	for _, filter := range site.filters {
		if filter.Serve != nil {
			site.serveFilters = append(site.serveFilters, filter.Serve)
		}
		if filter.Request != nil {
			site.requestFilters = append(site.requestFilters, filter.Request)
		}
		if filter.Execute != nil {
			site.executeFilters = append(site.executeFilters, filter.Execute)
		}
		if filter.Response != nil {
			site.responseFilters = append(site.responseFilters, filter.Response)
		}
	}

	site.notFoundHandlers = make([]ctxFunc, 0, len(site.handlers))
	site.errorHandlers = make([]ctxFunc, 0, len(site.handlers))
	site.failedHandlers = make([]ctxFunc, 0, len(site.handlers))
	site.unsignedHandlers = make([]ctxFunc, 0, len(site.handlers))
	site.unauthedHandlers = make([]ctxFunc, 0, len(site.handlers))
	site.deniedHandlers = make([]ctxFunc, 0, len(site.handlers))
	for _, handler := range site.handlers {
		if handler.NotFound != nil {
			site.notFoundHandlers = append(site.notFoundHandlers, handler.NotFound)
		}
		if handler.Error != nil {
			site.errorHandlers = append(site.errorHandlers, handler.Error)
		}
		if handler.Failed != nil {
			site.failedHandlers = append(site.failedHandlers, handler.Failed)
		}
		if handler.Unsigned != nil {
			site.unsignedHandlers = append(site.unsignedHandlers, handler.Unsigned)
		}
		if handler.Unauthed != nil {
			site.unauthedHandlers = append(site.unauthedHandlers, handler.Unauthed)
		}
		if handler.Denied != nil {
			site.deniedHandlers = append(site.deniedHandlers, handler.Denied)
		}
	}
}

func (m *Module) Open() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	driver := m.drivers[m.config.Driver]
	if driver == nil {
		panic("Invalid web driver: " + m.config.Driver)
	}

	inst := &Instance{
		Config:   m.config,
		Setting:  m.config.Setting,
		Delegate: m,
	}

	conn, err := driver.Connect(inst)
	if err != nil {
		panic("Failed to connect web: " + err.Error())
	}
	if err := conn.Open(); err != nil {
		panic("Failed to open web: " + err.Error())
	}

	for siteName, site := range m.sites {
		for _, routeName := range site.routerOrder {
			info := site.routerInfos[routeName]
			fullName := siteName + "." + routeName
			if err := conn.Register(fullName, info, nil); err != nil {
				panic("Failed to register web route: " + err.Error())
			}
		}
	}

	inst.connect = conn
	m.instance = inst
	m.opened = true
}

func (m *Module) Start() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.started {
		return
	}
	if m.instance != nil && m.instance.connect != nil {
		if m.config.CertFile != "" && m.config.KeyFile != "" {
			_ = m.instance.connect.StartTLS(m.config.CertFile, m.config.KeyFile)
		} else {
			_ = m.instance.connect.Start()
		}
	}
	m.started = true
	routeCount := 0
	for _, site := range m.sites {
		routeCount += len(site.routers)
	}
	connCount := 0
	if m.instance != nil && m.instance.connect != nil {
		connCount = 1
	}
	fmt.Printf("infrago web module is running with %d connections, %d sites, %d routers.\n", connCount, len(m.sites), routeCount)
}

func (m *Module) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.started {
		return
	}
	m.started = false
}

func (m *Module) Close() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.opened {
		return
	}

	if m.instance != nil && m.instance.connect != nil {
		_ = m.instance.connect.Close()
		m.instance.connect = nil
	}

	m.opened = false
}

// Serve implements Delegate to dispatch by host/site.
func (m *Module) Serve(name string, params Map, res http.ResponseWriter, req *http.Request) {
	selected, routerName := m.selectSiteForRequest(name, req.Host)
	site := m.sites[selected]
	if site == nil {
		return
	}
	site.Serve(routerName, params, res, req)
}

func (m *Module) selectSiteForRequest(name, host string) (string, string) {
	_, routerName := splitPrefix(name)

	// Site selection is host-driven:
	// - matched site alias => that site
	// - unmatched subdomain/ip/localhost => default(empty) site
	selected := m.resolveSiteByHost(host)
	if selected == "" {
		selected = m.defaultSite
	}

	if name == "" {
		routerName = ""
	} else if routerName == "" {
		routerName = name
	}

	return selected, routerName
}

func (m *Module) resolveSiteByHost(host string) string {
	host = normalizeHost(host)
	if host == "" {
		return ""
	}
	label := firstHostLabel(host)
	if label == "" {
		return ""
	}
	if site, ok := m.siteAliases[label]; ok {
		return site
	}
	return ""
}

func (m *Module) resolveSiteAliases(name string, cfg *Config) []string {
	aliases := make([]string, 0, 4)
	aliases = append(aliases, name)
	aliases = append(aliases, parseStringList(cfg.Alias)...)
	aliases = append(aliases, parseStringList(cfg.Aliases)...)

	uniq := make([]string, 0, len(aliases))
	exists := map[string]struct{}{}
	for _, alias := range aliases {
		alias = normalizeAlias(alias)
		if alias == "" {
			continue
		}
		if _, ok := exists[alias]; ok {
			continue
		}
		exists[alias] = struct{}{}
		uniq = append(uniq, alias)
	}

	if len(uniq) > 0 {
		cfg.Alias = uniq[0]
		if len(uniq) > 1 {
			cfg.Aliases = uniq[1:]
		} else {
			cfg.Aliases = nil
		}
	}

	return uniq
}

func (m *Module) resolveSiteHosts(name string, cfg *Config) []string {
	hosts := make([]string, 0, 4)
	hosts = append(hosts, parseStringList(cfg.Domain)...)
	hosts = append(hosts, parseStringList(cfg.Domains)...)

	if len(hosts) == 0 && m.config.Domain != "" {
		if name == infra.DEFAULT {
			hosts = append(hosts, m.config.Domain)
		} else {
			hosts = append(hosts, name+"."+m.config.Domain)
		}
	}

	uniq := make([]string, 0, len(hosts))
	exists := map[string]struct{}{}
	for _, host := range hosts {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if _, ok := exists[host]; ok {
			continue
		}
		exists[host] = struct{}{}
		uniq = append(uniq, host)
	}

	if len(uniq) > 0 {
		cfg.Domain = uniq[0]
		if len(uniq) > 1 {
			cfg.Domains = uniq[1:]
		} else {
			cfg.Domains = nil
		}
	}

	return uniq
}

func parseConfig(conf Map) Config {
	cfg := Config{}
	if v, ok := conf["driver"].(string); ok && v != "" {
		cfg.Driver = strings.ToLower(v)
	}
	if port, ok := parsePort(conf["port"]); ok {
		cfg.Port = port
	}
	if v, ok := conf["host"].(string); ok {
		cfg.Host = v
	}
	if v, ok := conf["bind"].(string); ok {
		cfg.Host = v
	}
	if v, ok := conf["readheadertimeout"]; ok {
		cfg.ReadHeaderTimeout = parseDuration(v)
	}
	if v, ok := conf["read_header_timeout"]; ok {
		cfg.ReadHeaderTimeout = parseDuration(v)
	}
	if v, ok := conf["readtimeout"]; ok {
		cfg.ReadTimeout = parseDuration(v)
	}
	if v, ok := conf["read_timeout"]; ok {
		cfg.ReadTimeout = parseDuration(v)
	}
	if v, ok := conf["writetimeout"]; ok {
		cfg.WriteTimeout = parseDuration(v)
	}
	if v, ok := conf["write_timeout"]; ok {
		cfg.WriteTimeout = parseDuration(v)
	}
	if v, ok := conf["idletimeout"]; ok {
		cfg.IdleTimeout = parseDuration(v)
	}
	if v, ok := conf["idle_timeout"]; ok {
		cfg.IdleTimeout = parseDuration(v)
	}
	if v, ok := conf["shutdowntimeout"]; ok {
		cfg.ShutdownTimeout = parseDuration(v)
	}
	if v, ok := conf["shutdown_timeout"]; ok {
		cfg.ShutdownTimeout = parseDuration(v)
	}
	if v, ok := parsePort(conf["maxbodybytes"]); ok && v > 0 {
		cfg.MaxBodyBytes = int64(v)
	}
	if v, ok := parsePort(conf["max_body_bytes"]); ok && v > 0 {
		cfg.MaxBodyBytes = int64(v)
	}
	if v, ok := conf["cert"].(string); ok {
		cfg.CertFile = v
	}
	if v, ok := conf["certfile"].(string); ok {
		cfg.CertFile = v
	}
	if v, ok := conf["key"].(string); ok {
		cfg.KeyFile = v
	}
	if v, ok := conf["keyfile"].(string); ok {
		cfg.KeyFile = v
	}
	if v, ok := conf["charset"].(string); ok {
		cfg.Charset = v
	}
	if v, ok := conf["cookie"].(string); ok {
		cfg.Cookie = v
	}
	if v, ok := conf["token"].(bool); ok {
		cfg.Token = v
		cfg.tokenSet = true
	}
	if v, ok := conf["expire"]; ok {
		if d := parseDuration(v); d > 0 {
			cfg.Expire = d
		}
	}
	if v, ok := conf["crypto"].(bool); ok {
		cfg.Crypto = v
		cfg.cryptoSet = true
	}
	if v, ok := conf["maxage"]; ok {
		if d := parseDuration(v); d > 0 {
			cfg.MaxAge = d
		}
	}
	if v, ok := conf["httponly"].(bool); ok {
		cfg.HttpOnly = v
		cfg.httpOnlySet = true
	}
	if v, ok := conf["answerencode"].(bool); ok {
		cfg.AnswerDataEncode = v
		cfg.answerDataEncodeSet = true
	}
	if v, ok := conf["answer_encode"].(bool); ok {
		cfg.AnswerDataEncode = v
		cfg.answerDataEncodeSet = true
	}
	if v, ok := conf["answerdataencode"].(bool); ok {
		cfg.AnswerDataEncode = v
		cfg.answerDataEncodeSet = true
	}
	if v, ok := conf["answer_data_encode"].(bool); ok {
		cfg.AnswerDataEncode = v
		cfg.answerDataEncodeSet = true
	}
	if v, ok := conf["answercodec"].(string); ok {
		cfg.AnswerDataCodec = strings.TrimSpace(v)
	}
	if v, ok := conf["answer_codec"].(string); ok {
		cfg.AnswerDataCodec = strings.TrimSpace(v)
	}
	if v, ok := conf["answerdatacodec"].(string); ok {
		cfg.AnswerDataCodec = strings.TrimSpace(v)
	}
	if v, ok := conf["answer_data_codec"].(string); ok {
		cfg.AnswerDataCodec = strings.TrimSpace(v)
	}
	if v, ok := conf["codec"].(string); ok {
		cfg.AnswerDataCodec = strings.TrimSpace(v)
	}
	if answer, ok := conf["answer"].(Map); ok && answer != nil {
		if v, ok := answer["encode"].(bool); ok {
			cfg.AnswerDataEncode = v
			cfg.answerDataEncodeSet = true
		}
		if v, ok := answer["answerencode"].(bool); ok {
			cfg.AnswerDataEncode = v
			cfg.answerDataEncodeSet = true
		}
		if v, ok := answer["codec"].(string); ok {
			cfg.AnswerDataCodec = strings.TrimSpace(v)
		}
		if v, ok := answer["answercodec"].(string); ok {
			cfg.AnswerDataCodec = strings.TrimSpace(v)
		}
	}
	if v, ok := conf["upload"].(string); ok {
		cfg.Upload = v
	}
	if v, ok := conf["static"].(string); ok {
		cfg.Static = v
	}
	if v, ok := conf["shared"].(string); ok {
		cfg.Shared = v
	}
	cfg.Defaults = parseStringList(conf["defaults"])

	domains := append([]string{}, parseStringList(conf["domain"])...)
	domains = append(domains, parseStringList(conf["domains"])...)
	if len(domains) > 0 {
		cfg.Domain = domains[0]
		if len(domains) > 1 {
			cfg.Domains = domains[1:]
		}
	}

	aliases := append([]string{}, parseStringList(conf["alias"])...)
	aliases = append(aliases, parseStringList(conf["aliases"])...)
	if len(aliases) > 0 {
		cfg.Alias = aliases[0]
		if len(aliases) > 1 {
			cfg.Aliases = aliases[1:]
		}
	}

	if v, ok := conf["setting"].(Map); ok {
		cfg.Setting = v
	}
	return cfg
}

func parseDuration(val Any) time.Duration {
	switch v := val.(type) {
	case time.Duration:
		return v
	case int:
		return time.Second * time.Duration(v)
	case int64:
		return time.Second * time.Duration(v)
	case float64:
		return time.Second * time.Duration(v)
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 0
}

func parsePort(val Any) (int, bool) {
	switch v := val.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func mergeConfig(baseCfg, newCfg Config) Config {
	out := baseCfg
	if newCfg.Driver != "" {
		out.Driver = newCfg.Driver
	}
	if newCfg.Port != 0 {
		out.Port = newCfg.Port
	}
	if newCfg.Host != "" {
		out.Host = newCfg.Host
	}
	if newCfg.ReadHeaderTimeout > 0 {
		out.ReadHeaderTimeout = newCfg.ReadHeaderTimeout
	}
	if newCfg.ReadTimeout > 0 {
		out.ReadTimeout = newCfg.ReadTimeout
	}
	if newCfg.WriteTimeout > 0 {
		out.WriteTimeout = newCfg.WriteTimeout
	}
	if newCfg.IdleTimeout > 0 {
		out.IdleTimeout = newCfg.IdleTimeout
	}
	if newCfg.ShutdownTimeout > 0 {
		out.ShutdownTimeout = newCfg.ShutdownTimeout
	}
	if newCfg.MaxBodyBytes > 0 {
		out.MaxBodyBytes = newCfg.MaxBodyBytes
	}
	if newCfg.CertFile != "" {
		out.CertFile = newCfg.CertFile
	}
	if newCfg.KeyFile != "" {
		out.KeyFile = newCfg.KeyFile
	}
	if newCfg.Charset != "" {
		out.Charset = newCfg.Charset
	}
	if newCfg.Cookie != "" {
		out.Cookie = newCfg.Cookie
	}
	if newCfg.tokenSet {
		out.Token = newCfg.Token
		out.tokenSet = true
	} else if newCfg.Token {
		out.Token = true
		out.tokenSet = true
	}
	if newCfg.Expire != 0 {
		out.Expire = newCfg.Expire
	}
	if newCfg.cryptoSet {
		out.Crypto = newCfg.Crypto
		out.cryptoSet = true
	} else if newCfg.Crypto {
		out.Crypto = true
		out.cryptoSet = true
	}
	if newCfg.MaxAge != 0 {
		out.MaxAge = newCfg.MaxAge
	}
	if newCfg.httpOnlySet {
		out.HttpOnly = newCfg.HttpOnly
		out.httpOnlySet = true
	} else if newCfg.HttpOnly {
		out.HttpOnly = true
		out.httpOnlySet = true
	}
	if newCfg.answerDataEncodeSet {
		out.AnswerDataEncode = newCfg.AnswerDataEncode
		out.answerDataEncodeSet = true
	} else if newCfg.AnswerDataEncode {
		out.AnswerDataEncode = true
		out.answerDataEncodeSet = true
	}
	if newCfg.AnswerDataCodec != "" {
		out.AnswerDataCodec = newCfg.AnswerDataCodec
	}
	if newCfg.Upload != "" {
		out.Upload = newCfg.Upload
	}
	if newCfg.Static != "" {
		out.Static = newCfg.Static
	}
	if newCfg.Shared != "" {
		out.Shared = newCfg.Shared
	}
	if len(newCfg.Defaults) > 0 {
		out.Defaults = newCfg.Defaults
	}
	if newCfg.Domain != "" {
		out.Domain = newCfg.Domain
	}
	if len(newCfg.Domains) > 0 {
		out.Domains = newCfg.Domains
	}
	if newCfg.Alias != "" {
		out.Alias = newCfg.Alias
	}
	if len(newCfg.Aliases) > 0 {
		out.Aliases = newCfg.Aliases
	}
	if newCfg.Setting != nil {
		out.Setting = newCfg.Setting
	}
	return out
}

func parseStringList(val Any) []string {
	switch v := val.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []Any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func firstString(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		if idx := strings.Index(host, "://"); idx > -1 {
			host = host[idx+3:]
		}
	}
	if i := strings.Index(host, "/"); i > -1 {
		host = host[:i]
	}
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	return strings.TrimSpace(host)
}

func normalizeAlias(alias string) string {
	alias = strings.TrimSpace(strings.ToLower(alias))
	if alias == "" {
		return ""
	}
	if strings.Contains(alias, ".") {
		alias = strings.SplitN(alias, ".", 2)[0]
	}
	return strings.TrimSpace(alias)
}

func firstHostLabel(host string) string {
	host = normalizeHost(host)
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) == 0 {
		return ""
	}
	return normalizeAlias(parts[0])
}

func hostDomain(host string) string {
	host = normalizeHost(host)
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	return strings.Join(parts[1:], ".")
}

func rootDomain(host string) string {
	host = normalizeHost(host)
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func siteContextName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || name == strings.ToLower(infra.DEFAULT) {
		return ""
	}
	return name
}

func splitPrefix(name string) (string, string) {
	name = strings.ToLower(name)
	if name == "" {
		return "", ""
	}
	if strings.HasPrefix(name, ".") {
		return infra.DEFAULT, strings.TrimPrefix(name, ".")
	}
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		return parts[0], parts[1]
	}
	return infra.DEFAULT, name
}

func crossMapValue(value Any) (Map, bool) {
	switch v := value.(type) {
	case Map:
		return v, v != nil
	default:
		return nil, false
	}
}

func mergeCross(base Cross, conf Map) Cross {
	out := base

	if v, ok := conf["allow"].(bool); ok {
		out.Allow = v
	}
	if v, ok := conf["enable"].(bool); ok {
		out.Allow = v
	}
	if v, ok := conf["method"].(string); ok {
		out.Method = v
	}
	if vals := parseStringList(conf["methods"]); len(vals) > 0 {
		out.Methods = vals
	}
	if v, ok := conf["origin"].(string); ok {
		out.Origin = v
	}
	if vals := parseStringList(conf["origins"]); len(vals) > 0 {
		out.Origins = vals
	}
	if v, ok := conf["header"].(string); ok {
		out.Header = v
	}
	if vals := parseStringList(conf["headers"]); len(vals) > 0 {
		out.Headers = vals
	}

	return out
}
