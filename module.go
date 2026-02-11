package web

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bamgoo/bamgoo"
	. "github.com/bamgoo/base"
)

func init() {
	bamgoo.Mount(module)
}

var module = &Module{
	defaultConfig: Config{Driver: DEFAULT, Charset: UTF8, Port: 8080},
	cross:         Cross{Allow: true},
	drivers:       make(map[string]Driver, 0),
	configs:       make(map[string]Config, 0),
	sites:         make(map[string]*Site, 0),
}

type (
	Module struct {
		mutex sync.Mutex

		opened  bool
		started bool

		defaultConfig Config
		cross         Cross

		drivers map[string]Driver
		config  Config
		configs map[string]Config
		sites   map[string]*Site

		instance *Instance
	}

	Config struct {
		Driver string
		Port   int
		Host   string

		CertFile string
		KeyFile  string

		Charset string

		Cookie   string
		Token    bool
		Expire   time.Duration
		Crypto   bool
		MaxAge   time.Duration
		HttpOnly bool

		Upload   string
		Static   string
		Defaults []string

		Domain  string
		Domains []string

		Setting Map
	}

	Configs map[string]Config

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

	Site struct {
		Name    string
		Config  Config
		Cross   Cross
		Setting Map

		routers  map[string]Router
		filters  map[string]Filter
		handlers map[string]Handler

		routerInfos map[string]Info

		serveFilters    []ctxFunc
		requestFilters  []ctxFunc
		executeFilters  []ctxFunc
		responseFilters []ctxFunc

		foundHandlers  []ctxFunc
		errorHandlers  []ctxFunc
		failedHandlers []ctxFunc
		deniedHandlers []ctxFunc
	}
)

// Register dispatches registrations.
func (m *Module) Register(name string, value Any) {
	switch v := value.(type) {
	case Driver:
		m.RegisterDriver(name, v)
	case Config:
		m.RegisterConfig(bamgoo.DEFAULT, v)
	case Configs:
		m.RegisterConfigs(v)
	case Router:
		m.RegisterRouter(name, v)
	case Filter:
		m.RegisterFilter(name, v)
	case Handler:
		m.RegisterHandler(name, v)
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

	if bamgoo.Override() {
		m.drivers[name] = driver
	} else {
		if _, ok := m.drivers[name]; !ok {
			m.drivers[name] = driver
		}
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
		name = bamgoo.DEFAULT
	}
	if bamgoo.Override() {
		m.configs[name] = config
	} else {
		if _, ok := m.configs[name]; !ok {
			m.configs[name] = config
		}
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

	cfgAny, ok := global["web"]
	if ok {
		if cfgMap, ok := cfgAny.(Map); ok && cfgMap != nil {
			m.configureRoot(cfgMap)
		}
	}

	siteAny, ok := global["site"]
	if !ok {
		return
	}
	siteMap, ok := siteAny.(Map)
	if !ok || siteMap == nil {
		return
	}
	for key, val := range siteMap {
		if conf, ok := val.(Map); ok && key != "setting" {
			m.configureSite(key, conf)
		}
	}
}

func (m *Module) configureRoot(conf Map) {
	cfg := m.defaultConfig
	if m.config.Driver != "" || m.config.Port != 0 {
		cfg = m.config
	}
	cfg = mergeConfig(cfg, parseConfig(conf))
	m.config = cfg
}

func (m *Module) configureSite(name string, conf Map) {
	cfg := m.defaultConfig
	if existing, ok := m.configs[name]; ok {
		cfg = existing
	}
	cfg = mergeConfig(cfg, parseConfig(conf))

	// domains
	if v, ok := conf["domain"].(string); ok && v != "" {
		cfg.Domain = v
	}
	if v, ok := conf["domains"].([]string); ok && len(v) > 0 {
		cfg.Domains = v
	}

	m.configs[name] = cfg
}

func (m *Module) ensureSite(name string) *Site {
	if name == "" {
		name = bamgoo.DEFAULT
	}
	site, ok := m.sites[name]
	if ok {
		return site
	}
	site = &Site{
		Name:     name,
		Config:   m.defaultConfig,
		Cross:    m.cross,
		Setting:  m.defaultConfig.Setting,
		routers:  make(map[string]Router, 0),
		filters:  make(map[string]Filter, 0),
		handlers: make(map[string]Handler, 0),
	}
	m.sites[name] = site
	return site
}

// Setup initializes defaults and sites.
func (m *Module) Setup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	if m.config.Driver == "" {
		m.config = m.defaultConfig
	}
	m.applyDefaults(&m.config)

	// ensure sites for configs
	if len(m.configs) == 0 {
		m.configs[bamgoo.DEFAULT] = m.defaultConfig
	}
	for name, cfg := range m.configs {
		site := m.ensureSite(name)
		site.Config = mergeConfig(site.Config, cfg)
		site.Setting = site.Config.Setting
		site.Cross = m.cross
		m.applyDefaults(&site.Config)
		if site.Config.Domain == "" && len(site.Config.Domains) == 0 {
			site.Config.Domain = name
		}
		m.buildSite(site)
	}

	for name, site := range m.sites {
		if _, ok := m.configs[name]; !ok {
			site.Config = mergeConfig(site.Config, m.defaultConfig)
			site.Setting = site.Config.Setting
			site.Cross = m.cross
			m.applyDefaults(&site.Config)
			if site.Config.Domain == "" && len(site.Config.Domains) == 0 {
				site.Config.Domain = name
			}
			m.buildSite(site)
		}
	}
}

func (m *Module) applyDefaults(cfg *Config) {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = 0
	}
	if cfg.Charset == "" {
		cfg.Charset = UTF8
	}
	if cfg.Defaults == nil || len(cfg.Defaults) == 0 {
		cfg.Defaults = []string{"index.html", "default.html"}
	}
	if cfg.Expire == 0 {
		cfg.Expire = time.Hour * 24 * 30
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = time.Hour * 24 * 30
	}
}

func (m *Module) buildSite(site *Site) {
	site.routerInfos = make(map[string]Info, 0)
	for key, router := range site.routers {
		for i, uri := range router.Uris {
			infoKey := key
			if i > 0 {
				infoKey = key + "." + string(rune('0'+i))
			}
			site.routerInfos[infoKey] = Info{
				Method: router.Method,
				Uri:    uri,
				Router: key,
				Args:   router.Args,
			}
		}
	}

	site.serveFilters = make([]ctxFunc, 0)
	site.requestFilters = make([]ctxFunc, 0)
	site.executeFilters = make([]ctxFunc, 0)
	site.responseFilters = make([]ctxFunc, 0)

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

	site.foundHandlers = make([]ctxFunc, 0)
	site.errorHandlers = make([]ctxFunc, 0)
	site.failedHandlers = make([]ctxFunc, 0)
	site.deniedHandlers = make([]ctxFunc, 0)

	for _, handler := range site.handlers {
		if handler.Found != nil {
			site.foundHandlers = append(site.foundHandlers, handler.Found)
		}
		if handler.Error != nil {
			site.errorHandlers = append(site.errorHandlers, handler.Error)
		}
		if handler.Failed != nil {
			site.failedHandlers = append(site.failedHandlers, handler.Failed)
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
		for routeName, info := range site.routerInfos {
			fullName := siteName + "." + routeName
			if err := conn.Register(fullName, info, site.Config.Domains, site.Config.Domain); err != nil {
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

// Serve implements Delegate to dispatch to site.
func (m *Module) Serve(name string, params Map, res http.ResponseWriter, req *http.Request) {
	siteName, routerName := splitPrefix(name)
	site := m.sites[siteName]
	if site == nil {
		site = m.sites[bamgoo.DEFAULT]
	}
	if site == nil {
		return
	}
	site.Serve(routerName, params, res, req)
}

func parseConfig(conf Map) Config {
	cfg := Config{}
	if v, ok := conf["driver"].(string); ok && v != "" {
		cfg.Driver = v
	}
	if v, ok := conf["port"].(int); ok {
		cfg.Port = v
	}
	if v, ok := conf["port"].(int64); ok {
		cfg.Port = int(v)
	}
	if v, ok := conf["port"].(float64); ok {
		cfg.Port = int(v)
	}
	if v, ok := conf["host"].(string); ok {
		cfg.Host = v
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
	}
	if v, ok := conf["expire"]; ok {
		if d := parseDuration(v); d > 0 {
			cfg.Expire = d
		}
	}
	if v, ok := conf["crypto"].(bool); ok {
		cfg.Crypto = v
	}
	if v, ok := conf["maxage"]; ok {
		if d := parseDuration(v); d > 0 {
			cfg.MaxAge = d
		}
	}
	if v, ok := conf["httponly"].(bool); ok {
		cfg.HttpOnly = v
	}
	if v, ok := conf["upload"].(string); ok {
		cfg.Upload = v
	}
	if v, ok := conf["static"].(string); ok {
		cfg.Static = v
	}
	if v, ok := conf["defaults"].([]string); ok {
		cfg.Defaults = v
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
	if newCfg.Token {
		out.Token = true
	}
	if newCfg.Expire != 0 {
		out.Expire = newCfg.Expire
	}
	if newCfg.Crypto {
		out.Crypto = true
	}
	if newCfg.MaxAge != 0 {
		out.MaxAge = newCfg.MaxAge
	}
	if newCfg.HttpOnly {
		out.HttpOnly = true
	}
	if newCfg.Upload != "" {
		out.Upload = newCfg.Upload
	}
	if newCfg.Static != "" {
		out.Static = newCfg.Static
	}
	if newCfg.Defaults != nil && len(newCfg.Defaults) > 0 {
		out.Defaults = newCfg.Defaults
	}
	if newCfg.Domain != "" {
		out.Domain = newCfg.Domain
	}
	if newCfg.Domains != nil && len(newCfg.Domains) > 0 {
		out.Domains = newCfg.Domains
	}
	if newCfg.Setting != nil {
		out.Setting = newCfg.Setting
	}
	return out
}

func splitPrefix(name string) (string, string) {
	name = strings.ToLower(name)
	if name == "" {
		return bamgoo.DEFAULT, ""
	}
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		return parts[0], parts[1]
	}
	return bamgoo.DEFAULT, name
}
