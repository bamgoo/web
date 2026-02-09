package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	. "github.com/bamgoo/base"
)

func init() {
	module.RegisterDriver(DEFAULT, &defaultDriver{})
}

type (
	defaultDriver struct{}

	defaultConnect struct {
		mutex    sync.RWMutex
		instance *Instance
		server   *http.Server
		router   *mux.Router
		routes   map[string]*mux.Route
	}
)

func (driver *defaultDriver) Connect(inst *Instance) (Connection, error) {
	return &defaultConnect{
		instance: inst,
		routes:   make(map[string]*mux.Route),
	}, nil
}

func (c *defaultConnect) Open() error {
	c.router = mux.NewRouter()
	c.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", c.instance.Config.Host, c.instance.Config.Port),
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      c.router,
	}

	c.router.NotFoundHandler = c
	c.router.MethodNotAllowedHandler = c

	return nil
}

func (c *defaultConnect) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	return c.server.Shutdown(ctx)
}

func (c *defaultConnect) Register(name string, info Info, domains []string, domain string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	useDomains := make([]string, 0)
	if len(domains) > 0 {
		useDomains = append(useDomains, domains...)
	} else if domain != "" {
		useDomains = append(useDomains, domain)
	}

	register := func(routeName string, r *mux.Router) {
		route := r.HandleFunc(info.Uri, c.ServeHTTP).Name(routeName)
		if info.Method != "" {
			route.Methods(info.Method)
		}
		c.routes[routeName] = route
	}

	if len(useDomains) == 0 {
		register(name, c.router)
		return nil
	}

	for _, host := range useDomains {
		if host == "" {
			continue
		}
		routeName := name + "#" + host
		sub := c.router.Host(host).Subrouter()
		register(routeName, sub)
	}
	return nil
}

func (c *defaultConnect) Start() error {
	if c.server == nil {
		panic("Invalid web server")
	}

	go func() {
		err := c.server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			panic(err.Error())
		}
	}()

	return nil
}

func (c *defaultConnect) StartTLS(certFile, keyFile string) error {
	if c.server == nil {
		panic("Invalid web server")
	}

	go func() {
		err := c.server.ListenAndServeTLS(certFile, keyFile)
		if err != nil && err != http.ErrServerClosed {
			panic(err.Error())
		}
	}()

	return nil
}

func (c *defaultConnect) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	name := ""
	params := Map{}

	route := mux.CurrentRoute(req)
	if route != nil {
		name = route.GetName()
		if idx := strings.Index(name, "#"); idx > 0 {
			name = name[:idx]
		}
		vars := mux.Vars(req)
		for k, v := range vars {
			params[k] = v
		}
	}

	if c.instance.Delegate != nil {
		c.instance.Delegate.Serve(name, params, res, req)
	}
}
