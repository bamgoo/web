package web

import (
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
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
	drivers:       make(map[string]Driver),
	configs:       make(map[string]Config),
	routers:       make(map[string]Router),
	filters:       make(map[string]Filter),
	handlers:      make(map[string]Handler),
	sites:         make(map[string]*Site),
	siteHosts:     make(map[string]string),
	defaultSite:   bamgoo.DEFAULT,
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

		routers  map[string]Router
		filters  map[string]Filter
		handlers map[string]Handler

		sites       map[string]*Site
		siteHosts   map[string]string
		defaultSite string

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
		Shared   string
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
		Hosts   []string

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
		m.RegisterConfig(name, v)
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
		name = bamgoo.DEFAULT
	}

	name = strings.ToLower(name)
	if bamgoo.Override() {
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
					if siteMap, ok := val.(Map); ok {
						siteRoot := Map{}
						for siteName, siteVal := range siteMap {
							if conf, ok := siteVal.(Map); ok {
								m.configureSite(siteName, conf)
							} else {
								siteRoot[siteName] = siteVal
							}
						}
						if len(siteRoot) > 0 {
							m.configureSite(bamgoo.DEFAULT, siteRoot)
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
		if siteMap, ok := siteAny.(Map); ok && siteMap != nil {
			root := Map{}
			for key, val := range siteMap {
				if conf, ok := val.(Map); ok {
					m.configureSite(key, conf)
				} else {
					root[key] = val
				}
			}
			if len(root) > 0 {
				m.configureSite(bamgoo.DEFAULT, root)
			}
		}
	}

	if crossAny, ok := global["cross"]; ok {
		if crossMap, ok := crossAny.(Map); ok && crossMap != nil {
			m.configureCross(crossMap)
		}
	}
}

func (m *Module) configureCross(conf Map) {
	if v, ok := conf["allow"].(bool); ok {
		m.cross.Allow = v
	}
	if v, ok := conf["method"].(string); ok {
		m.cross.Method = v
	}
	if vals := parseStringList(conf["methods"]); len(vals) > 0 {
		m.cross.Methods = vals
	}
	if v, ok := conf["origin"].(string); ok {
		m.cross.Origin = v
	}
	if vals := parseStringList(conf["origins"]); len(vals) > 0 {
		m.cross.Origins = vals
	}
	if v, ok := conf["header"].(string); ok {
		m.cross.Header = v
	}
	if vals := parseStringList(conf["headers"]); len(vals) > 0 {
		m.cross.Headers = vals
	}
}

func (m *Module) configureRoot(conf Map) {
	cfg := mergeConfig(m.defaultConfig, m.config)
	cfg = mergeConfig(cfg, parseConfig(conf))
	m.config = cfg
}

func (m *Module) configureSite(name string, conf Map) {
	name = strings.ToLower(name)
	cfg := mergeConfig(mergeConfig(m.defaultConfig, m.config), m.configs[name])
	cfg = mergeConfig(cfg, parseConfig(conf))
	m.configs[name] = cfg
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

	names := map[string]struct{}{bamgoo.DEFAULT: {}}
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

	m.sites = make(map[string]*Site, len(names))
	m.siteHosts = make(map[string]string, len(names)*2)
	m.defaultSite = bamgoo.DEFAULT

	for name := range names {
		baseCfg := mergeConfig(m.defaultConfig, m.config)
		if cfg, ok := m.configs[name]; ok {
			baseCfg = mergeConfig(baseCfg, cfg)
		}
		m.applyDefaults(&baseCfg)
		m.applySiteDefaults(name, &baseCfg)

		site := &Site{
			Name:     name,
			Config:   baseCfg,
			Cross:    m.cross,
			Setting:  baseCfg.Setting,
			routers:  make(map[string]Router),
			filters:  make(map[string]Filter),
			handlers: make(map[string]Handler),
		}
		site.Hosts = m.resolveSiteHosts(name, &site.Config)
		m.sites[name] = site
	}

	if _, ok := m.sites[bamgoo.DEFAULT]; !ok {
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
		for _, host := range site.Hosts {
			host = normalizeHost(host)
			if host == "" {
				continue
			}
			if bamgoo.Override() {
				m.siteHosts[host] = site.Name
			} else if _, ok := m.siteHosts[host]; !ok {
				m.siteHosts[host] = site.Name
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
	if m.config.Static != "" && cfg.Static == m.config.Static && name != bamgoo.DEFAULT {
		cfg.Static = path.Join(m.config.Static, name)
	}
	if len(cfg.Defaults) == 0 {
		cfg.Defaults = m.config.Defaults
	}
	if cfg.Shared == "" {
		cfg.Shared = m.config.Shared
	}
}

func (m *Module) buildSite(site *Site) {
	site.routerInfos = make(map[string]Info)
	for key, router := range site.routers {
		for i, uri := range router.Uris {
			infoKey := key
			if i > 0 {
				infoKey = key + "." + strconv.Itoa(i)
			}
			site.routerInfos[infoKey] = Info{
				Method: router.Method,
				Uri:    uri,
				Router: key,
				Args:   router.Args,
			}
		}
	}

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

	site.foundHandlers = make([]ctxFunc, 0, len(site.handlers))
	site.errorHandlers = make([]ctxFunc, 0, len(site.handlers))
	site.failedHandlers = make([]ctxFunc, 0, len(site.handlers))
	site.deniedHandlers = make([]ctxFunc, 0, len(site.handlers))
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
			if err := conn.Register(fullName, info, site.Hosts); err != nil {
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

// Serve implements Delegate to dispatch by host/site.
func (m *Module) Serve(name string, params Map, res http.ResponseWriter, req *http.Request) {
	siteName, routerName := splitPrefix(name)

	selected := ""
	if siteName != "" && siteName != bamgoo.DEFAULT {
		if _, ok := m.sites[siteName]; ok {
			selected = siteName
		}
	}
	if selected == "" {
		selected = m.resolveSiteByHost(req.Host)
	}
	if selected == "" {
		selected = m.defaultSite
	}
	site := m.sites[selected]
	if site == nil {
		return
	}

	if name == "" {
		routerName = ""
	} else if routerName == "" {
		routerName = name
	}
	site.Serve(routerName, params, res, req)
}

func (m *Module) resolveSiteByHost(host string) string {
	host = normalizeHost(host)
	if host == "" {
		return ""
	}
	if site, ok := m.siteHosts[host]; ok {
		return site
	}
	parts := strings.Split(host, ".")
	for i := 1; i < len(parts); i++ {
		pattern := "*." + strings.Join(parts[i:], ".")
		if site, ok := m.siteHosts[pattern]; ok {
			return site
		}
	}
	return ""
}

func (m *Module) resolveSiteHosts(name string, cfg *Config) []string {
	hosts := make([]string, 0, 4)
	hosts = append(hosts, parseStringList(cfg.Domain)...)
	hosts = append(hosts, parseStringList(cfg.Domains)...)

	if len(hosts) == 0 && m.config.Domain != "" {
		if name == bamgoo.DEFAULT {
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
	if v, ok := conf["bind"].(string); ok {
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
	if v, ok := conf["shared"].(string); ok {
		cfg.Shared = v
	}
	cfg.Defaults = parseStringList(conf["defaults"])
	cfg.Domain = firstString(parseStringList(conf["domain"]))
	cfg.Domains = parseStringList(conf["domains"])
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

func splitPrefix(name string) (string, string) {
	name = strings.ToLower(name)
	if name == "" {
		return "", ""
	}
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		return parts[0], parts[1]
	}
	return bamgoo.DEFAULT, name
}
