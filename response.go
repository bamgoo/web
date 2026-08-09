package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
	"github.com/infrago/view"
)

type (
	httpGotoBody struct {
		url string
	}
	httpTextBody struct {
		text string
	}
	httpHtmlBody struct {
		html string
	}
	httpJsonBody struct {
		json Any
	}
	httpJsonpBody struct {
		json     Any
		callback string
	}
	httpAnswerBody struct {
		code int
		text string
		data Map
	}
	httpFileBody struct {
		file string
		name string
	}
	httpBinaryBody struct {
		bytes []byte
		name  string
	}
	httpBufferBody struct {
		buffer io.ReadCloser
		size   int64
		name   string
	}
	httpProxyBody struct {
		target string
	}
	httpViewBody struct {
		view  string
		model Map
	}
	httpStatusBody string
)

func (site *webSite) bodyFail(ctx *Context, err error) {
	if err == nil {
		return
	}

	ctx.Result(infra.Fail.With(err.Error()))

	if ctx.output != nil && ctx.output.Committed() {
		if ctx.Code <= 0 {
			ctx.Code = ctx.output.Status()
		}
		return
	}

	if ctx.failedBody {
		if ctx.Code <= 0 {
			ctx.Code = StatusInternalServerError
		}
		ctx.writer.Header().Set("Content-Type", "text/plain; charset="+ctx.Charset())
		ctx.writer.WriteHeader(ctx.Code)
		_, _ = fmt.Fprint(ctx.writer, StatusText(ctx.Code))
		return
	}

	ctx.failedBody = true
	if ctx.Code <= 0 || ctx.Code < StatusInternalServerError {
		ctx.Code = StatusInternalServerError
	}
	ctx.handling = "error"
	site.handle(ctx)
	site.body(ctx)
}

func (site *webSite) body(ctx *Context) {
	if ctx.upgraded {
		return
	}

	if ctx.Code <= 0 {
		ctx.Code = StatusOK
	}

	// Write headers
	for k, v := range ctx.headers {
		ctx.writer.Header().Set(k, v)
	}

	// Write cookies
	for _, cookie := range ctx.cookies {
		http.SetCookie(ctx.writer, &cookie)
	}

	// Issue latest token into configured cookie when ctx.Sign/NewSign is called.
	if ctx.issue && ctx.site.Config.Cookie != "" {
		if token := ctx.Token(); token != "" {
			cookie := http.Cookie{
				Name:     ctx.site.Config.Cookie,
				Value:    token,
				Path:     "/",
				HttpOnly: ctx.site.Config.HttpOnly,
			}
			if ctx.site.Config.Domain != "" {
				cookie.Domain = ctx.site.Config.Domain
			}
			if ctx.Domain != "" {
				cookie.Domain = ctx.Domain
			}
			if ctx.site.Config.MaxAge > 0 {
				cookie.MaxAge = int(ctx.site.Config.MaxAge.Seconds())
			}
			http.SetCookie(ctx.writer, &cookie)
		}
	}

	switch body := ctx.Body.(type) {
	case string:
		site.bodyText(ctx, httpTextBody{body})
	case Map:
		site.bodyJson(ctx, httpJsonBody{body})
	case httpGotoBody:
		site.bodyGoto(ctx, body)
	case httpTextBody:
		site.bodyText(ctx, body)
	case httpHtmlBody:
		site.bodyHtml(ctx, body)
	case httpJsonBody:
		site.bodyJson(ctx, body)
	case httpJsonpBody:
		site.bodyJsonp(ctx, body)
	case httpAnswerBody:
		site.bodyAnswer(ctx, body)
	case httpFileBody:
		site.bodyFile(ctx, body)
	case httpBinaryBody:
		site.bodyBinary(ctx, body)
	case httpBufferBody:
		site.bodyBuffer(ctx, body)
	case httpProxyBody:
		site.bodyProxy(ctx, body)
	case httpViewBody:
		site.bodyView(ctx, body)
	case httpStatusBody:
		site.bodyStatus(ctx, body)
	default:
		site.bodyDefault(ctx)
	}
}

func (site *webSite) bodyDefault(ctx *Context) {
	if ctx.Code <= 0 {
		ctx.Code = StatusNotFound
		http.NotFound(ctx.writer, ctx.reader)
	} else {
		ctx.writer.WriteHeader(ctx.Code)
		fmt.Fprint(ctx.writer, StatusText(ctx.Code))
	}
}

func (site *webSite) bodyStatus(ctx *Context, body httpStatusBody) {
	if ctx.Code <= 0 {
		ctx.Code = StatusNotFound
		http.NotFound(ctx.writer, ctx.reader)
	} else {
		if body == "" {
			body = httpStatusBody(StatusText(ctx.Code))
		}
		ctx.writer.WriteHeader(ctx.Code)
		fmt.Fprint(ctx.writer, body)
	}
}

func (site *webSite) bodyGoto(ctx *Context, body httpGotoBody) {
	if ctx.Code <= 0 {
		ctx.Code = StatusFound
	}
	http.Redirect(ctx.writer, ctx.reader, body.url, StatusFound)
}

func (site *webSite) bodyText(ctx *Context, body httpTextBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "text"
	}

	mimeType := infra.Mimetype(ctx.Type, "text/plain")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	res.WriteHeader(ctx.Code)
	if _, err := fmt.Fprint(res, body.text); err != nil {
		site.bodyFail(ctx, err)
	}
}

func (site *webSite) bodyHtml(ctx *Context, body httpHtmlBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "html"
	}

	mimeType := infra.Mimetype(ctx.Type, "text/html")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	res.WriteHeader(ctx.Code)
	if _, err := fmt.Fprint(res, body.html); err != nil {
		site.bodyFail(ctx, err)
	}
}

func (site *webSite) bodyJson(ctx *Context, body httpJsonBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "json"
	}

	bytes, err := json.Marshal(body.json)
	if err != nil {
		site.bodyFail(ctx, err)
		return
	}

	mimeType := infra.Mimetype(ctx.Type, "application/json")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))
	res.WriteHeader(ctx.Code)
	if _, err := fmt.Fprint(res, string(bytes)); err != nil {
		site.bodyFail(ctx, err)
	}
}

func (site *webSite) bodyJsonp(ctx *Context, body httpJsonpBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "script"
	}

	bytes, err := json.Marshal(body.json)
	if err != nil {
		site.bodyFail(ctx, err)
		return
	}

	mimeType := infra.Mimetype(ctx.Type, "application/javascript")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	res.WriteHeader(ctx.Code)
	if _, err := fmt.Fprintf(res, "%s(%s);", body.callback, string(bytes)); err != nil {
		site.bodyFail(ctx, err)
	}
}

func (site *webSite) bodyAnswer(ctx *Context, body httpAnswerBody) {
	result := Map{
		"code": body.code,
		"time": time.Now().Unix(),
	}

	if body.text != "" {
		result["text"] = body.text
	}

	if body.data != nil {
		var data Any = body.data
		if ctx.Config.Data != nil && ctx.Code == StatusOK {
			val := Map{}
			res := infra.Mapping(Vars{
				"data": Var{Type: "json", Required: true, Children: ctx.Config.Data},
			}, Map{"data": body.data}, val, false, false, ctx.Timezone())
			if res == nil || res.OK() {
				if mapped, ok := val["data"]; ok {
					data = mapped
				}
			} else {
				result["code"] = infra.StatusCode(res.Status(), body.code)
				result["text"] = ctx.String(res.Status(), res.Args()...)
				site.bodyJson(ctx, httpJsonBody{result})
				return
			}
		}
		if ctx.site.Config.AnswerDataEncode {
			codec := strings.TrimSpace(ctx.site.Config.AnswerDataCodec)
			if codec == "" {
				codec = "text"
			}
			val := Map{}
			_ = infra.Mapping(Vars{
				"data": Var{Required: true, Encode: codec},
			}, Map{"data": data}, val, false, false, ctx.Timezone())
			if mapped, ok := val["data"]; ok {
				data = mapped
			}
		}
		result["data"] = data
	}

	site.bodyJson(ctx, httpJsonBody{result})
}

func (site *webSite) bodyFile(ctx *Context, body httpFileBody) {
	req, res := ctx.reader, ctx.writer

	if ctx.Type == "" {
		ctx.Type = "file"
	}

	mimeType := infra.Mimetype(ctx.Type, "application/octet-stream")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	if body.name != "" {
		res.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%v;", url.QueryEscape(body.name)))
	}

	http.ServeFile(res, req, body.file)
}

func (site *webSite) bodyBinary(ctx *Context, body httpBinaryBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "file"
	}

	mimeType := infra.Mimetype(ctx.Type, "application/octet-stream")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	if body.name != "" {
		res.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%v;", url.QueryEscape(body.name)))
	}

	res.WriteHeader(ctx.Code)
	if _, err := res.Write(body.bytes); err != nil {
		site.bodyFail(ctx, err)
	}
}

func (site *webSite) bodyBuffer(ctx *Context, body httpBufferBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "file"
	}

	mimeType := infra.Mimetype(ctx.Type, "application/octet-stream")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	if body.name != "" {
		res.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%v;", url.QueryEscape(body.name)))
	}

	if body.size > 0 {
		res.Header().Set("Content-Length", fmt.Sprintf("%d", body.size))
	}

	res.WriteHeader(ctx.Code)
	if _, err := io.Copy(res, body.buffer); err != nil {
		site.bodyFail(ctx, err)
	}
	body.buffer.Close()
}

func (site *webSite) bodyProxy(ctx *Context, body httpProxyBody) {
	target, err := url.Parse(strings.TrimSpace(body.target))
	if err != nil || target.Scheme == "" || target.Host == "" {
		site.bodyFail(ctx, fmt.Errorf("invalid proxy target %q", body.target))
		return
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			if prior, ok := pr.In.Header["X-Forwarded-For"]; ok {
				pr.Out.Header["X-Forwarded-For"] = append([]string(nil), prior...)
			}
			pr.SetURL(target)
			pr.SetXForwarded()
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			site.bodyFail(ctx, err)
		},
	}

	proxy.ServeHTTP(ctx.writer, ctx.reader)
}

func (site *webSite) bodyView(ctx *Context, body httpViewBody) {
	res := ctx.writer

	viewData := Map{
		"config":  ctx.site.Config,
		"setting": ctx.site.Setting,
		"args":    ctx.Args,
		"value":   ctx.Value,
		"locals":  ctx.Locals,
		"data":    ctx.Data,
		"model":   body.model,
	}

	html, err := view.Parse(view.Body{
		View:     body.view,
		Site:     site.Name,
		Helpers:  site.viewHelpers(ctx),
		Language: ctx.Language(),
		Timezone: ctx.Timezone(),
		Data:     viewData,
		Model:    body.model,
	})
	if err != nil {
		site.bodyFail(ctx, err)
		return
	}

	mimeType := infra.Mimetype(ctx.Type, "text/html")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))
	res.WriteHeader(ctx.Code)
	if _, err := fmt.Fprint(res, html); err != nil {
		site.bodyFail(ctx, err)
	}
}

func (site *webSite) viewHelpers(ctx *Context) Map {
	zone := ctx.Timezone()
	return Map{
		"language": func() string {
			return ctx.Language()
		},
		"timezone": func() string {
			return zone.String()
		},
		"format": func(format string, args ...interface{}) string {
			if len(args) == 1 {
				switch vv := args[0].(type) {
				case time.Time:
					return vv.In(zone).Format(format)
				case int64:
					// unix seconds range guard
					if vv >= 31507200 && vv <= 31507200000 {
						return time.Unix(vv, 0).In(zone).Format(format)
					}
				}
			}
			return fmt.Sprintf(format, args...)
		},
		"string": func(key string, args ...Any) string {
			return ctx.String(strings.ReplaceAll(key, ".", "_"), args...)
		},
		"ctx": func() *Context {
			return ctx
		},
	}
}
