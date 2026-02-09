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

	siteName, routerName := splitPrefix(name)
	site := m.ensureSite(siteName)

	if config.Uris == nil || len(config.Uris) == 0 {
		config.Uris = []string{config.Uri}
	} else if config.Uri != "" {
		config.Uris = append(config.Uris, config.Uri)
	}

	routers := make(map[string]Router, 0)

	// Handle routing by method
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

			// Copy from parent
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

			// Override from method config
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

	// Save routers
	for key, router := range routers {
		key = strings.ToLower(key)
		if bamgoo.Override() {
			site.routers[key] = router
		} else {
			if _, ok := site.routers[key]; !ok {
				site.routers[key] = router
			}
		}
	}
}

// RegisterFilter registers a web filter.
func (m *Module) RegisterFilter(name string, config Filter) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}
	instName, filterName := splitPrefix(name)
	site := m.ensureSite(instName)
	filterName = strings.ToLower(filterName)

	if bamgoo.Override() {
		site.filters[filterName] = config
	} else {
		if _, ok := site.filters[filterName]; !ok {
			site.filters[filterName] = config
		}
	}
}

// RegisterHandler registers a web handler.
func (m *Module) RegisterHandler(name string, config Handler) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}
	instName, handlerName := splitPrefix(name)
	site := m.ensureSite(instName)
	handlerName = strings.ToLower(handlerName)

	if bamgoo.Override() {
		site.handlers[handlerName] = config
	} else {
		if _, ok := site.handlers[handlerName]; !ok {
			site.handlers[handlerName] = config
		}
	}
}
