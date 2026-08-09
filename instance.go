package web

import (
	"net"
	"net/http"
	"os"
	"strings"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func (site *webSite) newContext() *Context {
	return &Context{
		site:        site,
		Meta:        infra.NewMeta(),
		uploadfiles: make([]string, 0),
		headers:     make(map[string]string, 0),
		cookies:     make(map[string]http.Cookie, 0),
		charset:     UTF8,
		Params:      Map{},
		Query:       Map{},
		Form:        Map{},
		Upload:      Map{},
		Value:       Map{},
		Args:        Map{},
		Locals:      Map{},
		Data:        Map{},
		Setting:     Map{},
	}
}

func (site *webSite) close(ctx *Context) {
	for _, file := range ctx.uploadfiles {
		os.Remove(file)
	}
}

// Serve handles incoming HTTP request.
func (site *webSite) Serve(name string, params Map, res http.ResponseWriter, req *http.Request) {
	ctx := site.newContext()
	out := wrapResponseWriter(res)

	ctx.reader = req
	ctx.writer = out
	ctx.output = out

	if info, ok := site.routerInfos[name]; ok {
		if info.Entry != "" {
			ctx.Name = info.Entry
		} else {
			ctx.Name = info.Router
		}
		if cfg, ok := site.routers[ctx.Name]; ok {
			ctx.Config = cfg
			ctx.Setting = cfg.Setting
		} else if cfg, ok := site.routers[info.Router]; ok {
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
	ctx.Site = siteContextName(site.Name)
	ctx.Domain = hostDomain(ctx.Host)
	ctx.RootDomain = rootDomain(ctx.Host)

	span := ctx.Begin("web:"+ctx.Name, infra.TraceAttrs("infrago", infra.TraceKindWeb, ctx.Name, Map{
		"module":    "web",
		"site":      site.Name,
		"operation": "serve",
		"method":    ctx.Method,
		"path":      ctx.Path,
		"host":      ctx.Host,
	}))
	ctx.Header("traceparent", ctx.TraceParent())
	defer func() {
		if ctx.Code >= StatusInternalServerError {
			span.End(infra.Fail.With("web status %d", ctx.Code))
			return
		}
		if res := ctx.Result(); res != nil && res.Fail() {
			span.End(res)
			return
		}
		span.End()
	}()

	site.open(ctx)
	if ctx.output != nil && ctx.output.Committed() {
		ctx.Code = ctx.output.Status()
	}
	site.close(ctx)
}

func (site *webSite) open(ctx *Context) {
	ctx.clear()

	ctx.next(site.preprocessing)
	ctx.next(site.serveFilters...)
	ctx.next(site.serve)

	ctx.Next()
	// A Serve filter runs before route lookup and may intentionally terminate
	// the chain with a response (for example CORS or maintenance policies).
	// Flush that response even though the normal serve phase was skipped.
	if ctx.output != nil && !ctx.output.Committed() && ctx.Body != nil {
		site.response(ctx)
	}
}

func (site *webSite) serve(ctx *Context) {
	ctx.clear()

	ctx.next(site.crossing)
	ctx.next(site.finding)
	ctx.next(site.requestFilters...)
	ctx.next(site.request)

	ctx.Next()

	site.handle(ctx)
	site.response(ctx)
}

func (site *webSite) handle(ctx *Context) {
	handling := ctx.handling
	ctx.handling = ""
	switch handling {
	case "notfound":
		site.notFound(ctx)
	case "error":
		site.error(ctx)
	case "failed":
		site.failed(ctx)
	case "unsigned":
		site.unsigned(ctx)
	case "unauthed":
		site.unauthed(ctx)
	case "denied":
		site.denied(ctx)
	}
}

func (site *webSite) request(ctx *Context) {
	ctx.clear()

	ctx.next(site.parsing)
	ctx.next(site.authorizing)
	ctx.next(site.arguing)
	ctx.next(site.loading)
	ctx.next(site.execute)

	ctx.Next()
}

func (site *webSite) execute(ctx *Context) {
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

func (site *webSite) response(ctx *Context) {
	if ctx.upgraded {
		return
	}

	ctx.clear()

	ctx.next(site.responseFilters...)
	ctx.next(site.body)
	ctx.Next()
}

func (site *webSite) notFound(ctx *Context) {
	ctx.clear()

	if ctx.Code <= 0 {
		ctx.Code = StatusNotFound
	}

	if ctx.Config.NotFound != nil {
		ctx.next(ctx.Config.NotFound)
	}
	ctx.next(site.notFoundHandlers...)
	ctx.next(site.foundDefault)

	ctx.Next()
}

func (site *webSite) foundDefault(ctx *Context) {
	ctx.Text("Not Found", StatusNotFound)
}

func (site *webSite) error(ctx *Context) {
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

func (site *webSite) errorDefault(ctx *Context) {
	ctx.Text("Internal Server Error", StatusInternalServerError)
}

func (site *webSite) failed(ctx *Context) {
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

func (site *webSite) failedDefault(ctx *Context) {
	ctx.Text("Bad Request", StatusBadRequest)
}

func (site *webSite) denied(ctx *Context) {
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

func (site *webSite) unsigned(ctx *Context) {
	ctx.clear()

	if ctx.Code <= 0 {
		ctx.Code = StatusUnauthorized
	}

	if ctx.Config.Unsigned != nil {
		ctx.next(ctx.Config.Unsigned)
	}
	ctx.next(site.unsignedHandlers...)
	ctx.next(site.deniedHandlers...)
	ctx.next(site.deniedDefault)

	ctx.Next()
}

func (site *webSite) unauthed(ctx *Context) {
	ctx.clear()

	if ctx.Code <= 0 {
		ctx.Code = StatusUnauthorized
	}

	if ctx.Config.Unauthed != nil {
		ctx.next(ctx.Config.Unauthed)
	}
	ctx.next(site.unauthedHandlers...)
	ctx.next(site.deniedHandlers...)
	ctx.next(site.deniedDefault)

	ctx.Next()
}

func (site *webSite) deniedDefault(ctx *Context) {
	ctx.Text("Unauthorized", StatusUnauthorized)
}
