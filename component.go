package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/bamgoo/bamgoo"
	. "github.com/bamgoo/base"
)

type (
	Cookie = http.Cookie

	// Router defines HTTP route.
	Router struct {
		Method   string
		Uri      string   `json:"uri"`
		Uris     []string `json:"uris"`
		Name     string   `json:"name"`
		Desc     string   `json:"desc"`
		Nullable bool     `json:"-"`
		Args     Vars     `json:"args"`
		Data     Vars     `json:"data"`
		Setting  Map      `json:"-"`

		Routing Routing   `json:"routing"`
		Actions []ctxFunc `json:"-"`
		Action  ctxFunc   `json:"-"`

		Sign bool `json:"sign"`
		Auth bool `json:"auth"`

		Found  ctxFunc `json:"-"`
		Error  ctxFunc `json:"-"`
		Failed ctxFunc `json:"-"`
		Denied ctxFunc `json:"-"`
	}

	Routing map[string]Router

	// Filter defines HTTP filter/interceptor.
	Filter struct {
		Name     string  `json:"name"`
		Desc     string  `json:"desc"`
		Serve    ctxFunc `json:"-"`
		Request  ctxFunc `json:"-"`
		Execute  ctxFunc `json:"-"`
		Response ctxFunc `json:"-"`
	}

	// Handler defines HTTP handler for errors.
	Handler struct {
		Name   string  `json:"name"`
		Desc   string  `json:"desc"`
		Found  ctxFunc `json:"-"`
		Error  ctxFunc `json:"-"`
		Failed ctxFunc `json:"-"`
		Denied ctxFunc `json:"-"`
	}

	// File represents uploaded file info.
	File struct {
		Checksum  string `json:"checksum"`
		Filename  string `json:"filename"`
		Extension string `json:"extension"`
		Mimetype  string `json:"mimetype"`
		Length    int64  `json:"length"`
		Tempfile  string `json:"tempfile"`
	}
)

// RegisterRouter registers a web router.
func (m *Module) RegisterRouter(name string, config Router) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	name = strings.ToLower(name)
	if bamgoo.Override() {
		m.routers[name] = config
	} else if _, ok := m.routers[name]; !ok {
		m.routers[name] = config
	}
}

// RegisterFilter registers a web filter.
func (m *Module) RegisterFilter(name string, config Filter) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	name = strings.ToLower(name)
	if bamgoo.Override() {
		m.filters[name] = config
	} else if _, ok := m.filters[name]; !ok {
		m.filters[name] = config
	}
}

// RegisterHandler registers a web handler.
func (m *Module) RegisterHandler(name string, config Handler) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	name = strings.ToLower(name)
	if bamgoo.Override() {
		m.handlers[name] = config
	} else if _, ok := m.handlers[name]; !ok {
		m.handlers[name] = config
	}
}

func applyRouter(site *Site, routerName string, config Router) {
	routers := expandRouter(routerName, config)
	storeRouters(site.routers, routers)
}

func expandRouter(routerName string, config Router) map[string]Router {
	if config.Uris == nil || len(config.Uris) == 0 {
		config.Uris = []string{config.Uri}
	} else if config.Uri != "" {
		config.Uris = append(config.Uris, config.Uri)
	}

	routers := make(map[string]Router)

	if config.Routing != nil {
		for method, methodConfig := range config.Routing {
			realName := fmt.Sprintf("%s.%s", routerName, method)
			realConfig := config

			realConfig.Method = method
			realConfig.Nullable = methodConfig.Nullable
			realConfig.Action = nil
			realConfig.Actions = nil
			realConfig.Routing = nil
			realConfig.Args = nil
			realConfig.Data = nil
			realConfig.Setting = nil

			if config.Args != nil {
				realConfig.Args = Vars{}
				for k, v := range config.Args {
					realConfig.Args[k] = v
				}
			}
			if config.Data != nil {
				realConfig.Data = Vars{}
				for k, v := range config.Data {
					realConfig.Data[k] = v
				}
			}
			if config.Setting != nil {
				realConfig.Setting = Map{}
				for k, v := range config.Setting {
					realConfig.Setting[k] = v
				}
			}

			if methodConfig.Name != "" {
				realConfig.Name = methodConfig.Name
			}
			if methodConfig.Desc != "" {
				realConfig.Desc = methodConfig.Desc
			}
			if methodConfig.Args != nil {
				if realConfig.Args == nil {
					realConfig.Args = Vars{}
				}
				for k, v := range methodConfig.Args {
					realConfig.Args[k] = v
				}
			}
			if methodConfig.Data != nil {
				if realConfig.Data == nil {
					realConfig.Data = Vars{}
				}
				for k, v := range methodConfig.Data {
					realConfig.Data[k] = v
				}
			}
			if methodConfig.Setting != nil {
				if realConfig.Setting == nil {
					realConfig.Setting = Map{}
				}
				for k, v := range methodConfig.Setting {
					realConfig.Setting[k] = v
				}
			}

			if methodConfig.Action != nil {
				realConfig.Action = methodConfig.Action
			}
			if methodConfig.Actions != nil {
				realConfig.Actions = methodConfig.Actions
			}
			if methodConfig.Found != nil {
				realConfig.Found = methodConfig.Found
			}
			if methodConfig.Error != nil {
				realConfig.Error = methodConfig.Error
			}
			if methodConfig.Failed != nil {
				realConfig.Failed = methodConfig.Failed
			}
			if methodConfig.Denied != nil {
				realConfig.Denied = methodConfig.Denied
			}

			routers[realName] = realConfig
		}
		config.Routing = nil
	}

	if config.Action != nil {
		routerName += ".*"
		routers[routerName] = config
	}

	return routers
}

func storeRouters(target map[string]Router, routers map[string]Router) {
	for key, router := range routers {
		key = strings.ToLower(key)
		if bamgoo.Override() {
			target[key] = router
		} else if _, ok := target[key]; !ok {
			target[key] = router
		}
	}
}

func storeFilter(target map[string]Filter, name string, config Filter) {
	name = strings.ToLower(name)
	if bamgoo.Override() {
		target[name] = config
	} else if _, ok := target[name]; !ok {
		target[name] = config
	}
}

func storeHandler(target map[string]Handler, name string, config Handler) {
	name = strings.ToLower(name)
	if bamgoo.Override() {
		target[name] = config
	} else if _, ok := target[name]; !ok {
		target[name] = config
	}
}
