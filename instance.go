package web

import (
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/bamgoo/bamgoo"
	. "github.com/bamgoo/base"
)

func (site *Site) newContext() *Context {
	return &Context{
		site:       site,
		Meta:       bamgoo.NewMeta(),
		uploadfiles: make([]string, 0),
		headers:    make(map[string]string, 0),
		cookies:    make(map[string]http.Cookie, 0),
		charset:    UTF8,
		Params:     Map{},
		Query:      Map{},
		Form:       Map{},
		Upload:     Map{},
		Value:      Map{},
		Args:       Map{},
		Locals:     Map{},
		Data:       Map{},
		Setting:    Map{},
	}
}

func (site *Site) close(ctx *Context) {
	for _, file := range ctx.uploadfiles {
		os.Remove(file)
	}
}

// Serve handles incoming HTTP request.
func (site *Site) Serve(name string, params Map, res http.ResponseWriter, req *http.Request) {
	ctx := site.newContext()

	ctx.reader = req
	ctx.writer = res

	if info, ok := site.routerInfos[name]; ok {
		ctx.Name = info.Router
		if cfg, ok := site.routers[ctx.Name]; ok {
			ctx.Config = cfg
			ctx.Setting = cfg.Setting
		}
	}

	ctx.Params = params
	ctx.Method = strings.ToUpper(ctx.reader.Method)
	ctx.Uri = ctx.reader.RequestURI
	ctx.Path = ctx.reader.URL.Path

	if strings.Contains(ctx.reader.Host, ":") {
		host, _, err := net.SplitHostPort(ctx.reader.Host)
		if err == nil {
			ctx.Host = host
		}
	} else {
		ctx.Host = ctx.reader.Host
	}

	site.open(ctx)
	site.close(ctx)
}

func (site *Site) open(ctx *Context) {
	ctx.clear()

	ctx.next(site.preprocessing)
	ctx.next(site.serveFilters...)
	ctx.next(site.serve)

	ctx.Next()
}

func (site *Site) serve(ctx *Context) {
	ctx.clear()

	ctx.next(site.finding)
	ctx.next(site.requestFilters...)
	ctx.next(site.request)

	ctx.Next()

	site.response(ctx)
}

func (site *Site) request(ctx *Context) {
	ctx.clear()

	ctx.next(site.crossing)
	ctx.next(site.parsing)
	ctx.next(site.authorizing)
	ctx.next(site.arguing)
	ctx.next(site.execute)

	ctx.Next()
}

func (site *Site) execute(ctx *Context) {
	ctx.clear()

	ctx.next(site.executeFilters...)
	if ctx.Config.Actions != nil && len(ctx.Config.Actions) > 0 {
		ctx.next(ctx.Config.Actions...)
	}
	if ctx.Config.Action != nil {
		ctx.next(ctx.Config.Action)
	}

	ctx.Next()
}

func (site *Site) response(ctx *Context) {
	ctx.clear()

	ctx.next(site.responseFilters...)
	ctx.Next()

	site.body(ctx)
}

func (site *Site) found(ctx *Context) {
	ctx.clear()

	if ctx.Code <= 0 {
		ctx.Code = StatusNotFound
	}

	if ctx.Config.Found != nil {
		ctx.next(ctx.Config.Found)
	}
	ctx.next(site.foundHandlers...)
	ctx.next(site.foundDefault)

	ctx.Next()
}

func (site *Site) foundDefault(ctx *Context) {
	ctx.Text("Not Found", StatusNotFound)
}

func (site *Site) error(ctx *Context) {
	ctx.clear()

	if ctx.Code <= 0 {
		ctx.Code = StatusInternalServerError
	}

	if ctx.Config.Error != nil {
		ctx.next(ctx.Config.Error)
	}
	ctx.next(site.errorHandlers...)
	ctx.next(site.errorDefault)

	ctx.Next()
}

func (site *Site) errorDefault(ctx *Context) {
	ctx.Text("Internal Server Error", StatusInternalServerError)
}

func (site *Site) failed(ctx *Context) {
	ctx.clear()

	if ctx.Code <= 0 {
		ctx.Code = StatusBadRequest
	}

	if ctx.Config.Failed != nil {
		ctx.next(ctx.Config.Failed)
	}
	ctx.next(site.failedHandlers...)
	ctx.next(site.failedDefault)

	ctx.Next()
}

func (site *Site) failedDefault(ctx *Context) {
	ctx.Text("Bad Request", StatusBadRequest)
}

func (site *Site) denied(ctx *Context) {
	ctx.clear()

	if ctx.Code <= 0 {
		ctx.Code = StatusUnauthorized
	}

	if ctx.Config.Denied != nil {
		ctx.next(ctx.Config.Denied)
	}
	ctx.next(site.deniedHandlers...)
	ctx.next(site.deniedDefault)

	ctx.Next()
}

func (site *Site) deniedDefault(ctx *Context) {
	ctx.Text("Unauthorized", StatusUnauthorized)
}
