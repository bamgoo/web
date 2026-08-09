package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	. "github.com/infrago/base"
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

	defaultSocketConn struct {
		conn *websocket.Conn
	}
)

func (driver *defaultDriver) Connect(inst *Instance) (Connection, error) {
	return &defaultConnect{
		instance: inst,
		routes:   make(map[string]*mux.Route),
	}, nil
}

func (c *defaultConnect) Open() error {
	readHeaderTimeout := c.instance.Config.ReadHeaderTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = 5 * time.Second
	}
	readTimeout := c.instance.Config.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 15 * time.Second
	}
	writeTimeout := c.instance.Config.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = 15 * time.Second
	}
	idleTimeout := c.instance.Config.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	c.router = mux.NewRouter()
	c.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", c.instance.Config.Host, c.instance.Config.Port),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		Handler:           c.router,
	}

	c.router.NotFoundHandler = c
	c.router.MethodNotAllowedHandler = c

	return nil
}

func (c *defaultConnect) Close() error {
	shutdownTimeout := c.instance.Config.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return c.server.Shutdown(ctx)
}

func (c *defaultConnect) Register(name string, info Info, hosts []string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	useHosts := make([]string, 0, len(hosts))
	useHosts = append(useHosts, hosts...)

	register := func(routeName string, r *mux.Router) {
		route := r.HandleFunc(info.Uri, c.ServeHTTP).Name(routeName)
		if info.Method != "" {
			route.Methods(info.Method)
		}
		c.routes[routeName] = route
	}

	if len(useHosts) == 0 {
		register(name, c.router)
		return nil
	}

	for _, host := range useHosts {
		if host == "" {
			continue
		}
		routeName := name + "#" + host
		host = normalizeHostPattern(host)
		sub := c.router.Host(host).Subrouter()
		register(routeName, sub)
	}
	return nil
}

func (c *defaultConnect) Upgrade(res http.ResponseWriter, req *http.Request) (Socket, error) {
	upgrader := websocket.Upgrader{
		CheckOrigin:       func(*http.Request) bool { return true },
		EnableCompression: true,
	}
	conn, err := upgrader.Upgrade(res, req, nil)
	if err != nil {
		return nil, err
	}
	return &defaultSocketConn{conn: conn}, nil
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

func normalizeHostPattern(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if strings.HasPrefix(host, "*.") {
		return "{subdomain:[^.]+}" + host[1:]
	}
	return host
}

func (c *defaultSocketConn) ReadMessage() (int, []byte, error) {
	return c.conn.ReadMessage()
}

func (c *defaultSocketConn) WriteMessage(messageType int, data []byte) error {
	return c.conn.WriteMessage(messageType, data)
}

func (c *defaultSocketConn) Close() error {
	return c.conn.Close()
}

func (c *defaultSocketConn) Raw() Any {
	return c.conn
}

func (c *defaultSocketConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *defaultSocketConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *defaultSocketConn) SetReadLimit(limit int64) {
	c.conn.SetReadLimit(limit)
}

func (c *defaultSocketConn) SetPongHandler(fn func(string) error) {
	c.conn.SetPongHandler(fn)
}

func (c *defaultSocketConn) EnableWriteCompression(enabled bool) {
	c.conn.EnableWriteCompression(enabled)
}

func (c *defaultSocketConn) SetCompressionLevel(level int) error {
	return c.conn.SetCompressionLevel(level)
}
