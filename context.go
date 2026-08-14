package web

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

type (
	Context struct {
		site *webSite
		*infra.Meta

		uploadfiles []string

		index int
		nexts []ctxFunc

		reader *http.Request
		writer http.ResponseWriter
		output *responseWriter

		Name    string
		Config  Router
		Setting Map

		charset string
		headers map[string]string
		cookies map[string]http.Cookie
		issue   bool

		Method     string
		Host       string
		Site       string
		Domain     string
		RootDomain string
		Path       string
		Uri        string

		Ajax bool

		Params Map
		Query  Map
		Form   Map
		Upload Map

		Value  Map
		Args   Map
		Locals Map
		// RequestBody preserves the exact bytes of non-multipart request bodies.
		// It is useful for verifying signed webhook payloads after parsing.
		RequestBody []byte

		Code int
		Type string
		Data Map
		Body Any

		handling   string
		failedBody bool
		upgraded   bool
	}

	ctxFunc func(*Context)
)

func (ctx *Context) clear() {
	ctx.index = 0
	ctx.nexts = make([]ctxFunc, 0)
}

func (ctx *Context) next(nexts ...ctxFunc) {
	ctx.nexts = append(ctx.nexts, nexts...)
}

func (ctx *Context) Next() {
	if len(ctx.nexts) > ctx.index {
		next := ctx.nexts[ctx.index]
		ctx.index++
		if next != nil {
			next(ctx)
		} else {
			ctx.Next()
		}
	}
}

func (ctx *Context) abort() {
	ctx.index = len(ctx.nexts)
}

func (ctx *Context) NotFound() {
	ctx.handling = "notfound"
	ctx.abort()
}

func (ctx *Context) Error(args ...Res) {
	res := infra.Fail
	if len(args) > 0 && args[0] != nil {
		res = args[0]
	}
	ctx.Result(res)
	ctx.handling = "error"
	ctx.abort()
}

func (ctx *Context) Fail(args ...Res) {
	res := infra.Fail
	if len(args) > 0 && args[0] != nil {
		res = args[0]
	}
	ctx.Result(res)
	ctx.handling = "failed"
	ctx.abort()
}

func (ctx *Context) Unsign(args ...Res) {
	res := infra.Unsigned
	if len(args) > 0 && args[0] != nil {
		res = args[0]
	}
	ctx.Result(res)
	ctx.handling = "unsigned"
	ctx.abort()
}

func (ctx *Context) Unauth(args ...Res) {
	res := infra.Unauthed
	if len(args) > 0 && args[0] != nil {
		res = args[0]
	}
	ctx.Result(res)
	ctx.handling = "unauthed"
	ctx.abort()
}

func (ctx *Context) Deny(args ...Res) {
	res := infra.Denied
	if len(args) > 0 && args[0] != nil {
		res = args[0]
	}
	ctx.Result(res)
	ctx.handling = "denied"
	ctx.abort()
}

func (ctx *Context) Charset(charsets ...string) string {
	if len(charsets) > 0 && charsets[0] != "" {
		ctx.charset = charsets[0]
	}
	if ctx.charset == "" {
		ctx.charset = UTF8
	}
	return ctx.charset
}

func (ctx *Context) Header(key string, vals ...string) string {
	if len(vals) > 0 {
		ctx.headers[key] = vals[0]
		return vals[0]
	}
	return ctx.reader.Header.Get(key)
}

func (ctx *Context) Cookie(key string, vals ...Any) string {
	if len(vals) > 0 {
		vvv := vals[0]
		if vvv == nil {
			cookie := ctx.defaultCookie(key, "")
			cookie.MaxAge = -1
			ctx.cookies[key] = cookie
			return ""
		}
		switch val := vvv.(type) {
		case http.Cookie:
			if val.Name == "" {
				val.Name = key
			}
			ctx.cookies[key] = val
		case string:
			ctx.cookies[key] = ctx.defaultCookie(key, val)
		}
		return ""
	}

	c, err := ctx.reader.Cookie(key)
	if err == nil {
		return c.Value
	}
	return ""
}

func (ctx *Context) defaultCookie(key, value string) http.Cookie {
	cookie := http.Cookie{Name: key, Value: value, Path: "/"}
	if ctx.site != nil {
		cookie.HttpOnly = ctx.site.Config.HttpOnly
		cookie.Domain = ctx.site.Config.Domain
		if ctx.site.Config.MaxAge > 0 {
			cookie.MaxAge = int(ctx.site.Config.MaxAge.Seconds())
		}
	}
	return cookie
}

// Sign issues token and marks cookie issuance.
// expires is optional duration, begin defaults to current time.
func (ctx *Context) Sign(auth bool, payload Map, expires ...time.Duration) string {
	token := ctx.Meta.Sign(auth, payload, expires...)
	if token != "" {
		ctx.issue = ctx.site.Config.Cookie != ""
	}
	return token
}

// SignAt issues token with custom begin time and marks cookie issuance.
// expires is optional duration.
func (ctx *Context) SignAt(auth bool, payload Map, begin time.Time, expires ...time.Duration) string {
	token := ctx.Meta.SignAt(auth, payload, begin, expires...)
	if token != "" {
		ctx.issue = ctx.site.Config.Cookie != ""
	}
	return token
}

// NewSign issues token with new token id and marks cookie issuance.
// expires is optional duration, begin defaults to current time.
func (ctx *Context) NewSign(auth bool, payload Map, expires ...time.Duration) string {
	token := ctx.Meta.NewSign(auth, payload, expires...)
	if token != "" {
		ctx.issue = ctx.site.Config.Cookie != ""
	}
	return token
}

// NewSignAt issues token with new token id and custom begin time.
// expires is optional duration.
func (ctx *Context) NewSignAt(auth bool, payload Map, begin time.Time, expires ...time.Duration) string {
	token := ctx.Meta.NewSignAt(auth, payload, begin, expires...)
	if token != "" {
		ctx.issue = ctx.site.Config.Cookie != ""
	}
	return token
}

func (ctx *Context) IP() string {
	ip := "127.0.0.1"

	if forwarded := ctx.reader.Header.Get("x-forwarded-for"); forwarded != "" {
		ip = forwarded
	} else if realIp := ctx.reader.Header.Get("X-Real-IP"); realIp != "" {
		ip = realIp
	} else {
		ip = ctx.reader.RemoteAddr
	}

	if newip, _, err := net.SplitHostPort(ip); err == nil {
		ip = newip
	}

	ips := strings.Split(ip, ", ")
	if len(ips) > 0 {
		return ips[len(ips)-1]
	}
	return ip
}

func (ctx *Context) Agent() string {
	return ctx.Header("User-Agent")
}

func (ctx *Context) RouteUri(name string, values ...Map) string {
	return (&webUrl{ctx: ctx}).RouteUri(name, values...)
}

func (ctx *Context) RouteUrl(name string, values ...Map) string {
	return (&webUrl{ctx: ctx}).RouteUrl(name, values...)
}

func (ctx *Context) SiteUrl(name, path string, options ...Map) string {
	return (&webUrl{ctx: ctx}).SiteUrl(name, path, options...)
}

// Response methods

func (ctx *Context) clearBody() {
	if vv, ok := ctx.Body.(httpBufferBody); ok {
		vv.buffer.Close()
	}
}

func (ctx *Context) codingTyping(def string, args ...Any) {
	code := 0
	tttt := ""
	for _, arg := range args {
		if vv, ok := arg.(int); ok {
			code = vv
		}
		if vv, ok := arg.(string); ok {
			tttt = vv
		}
	}
	if code > 0 {
		ctx.Code = code
	}
	if ctx.Type == "" {
		if tttt != "" {
			ctx.Type = tttt
		} else {
			ctx.Type = def
		}
	} else if tttt != "" {
		ctx.Type = tttt
	}
}

func (ctx *Context) Goto(url string) {
	ctx.clearBody()
	ctx.Body = httpGotoBody{url}
}

func (ctx *Context) Redirect(url string) {
	ctx.Goto(url)
}

func (ctx *Context) Text(text Any, args ...Any) {
	ctx.clearBody()
	ctx.codingTyping("text", args...)

	real := ""
	if res, ok := text.(Res); ok {
		real = ctx.String(res.Status(), res.Args()...)
	} else if vv, ok := text.(string); ok {
		real = vv
	} else {
		real = fmt.Sprintf("%v", text)
	}
	ctx.Body = httpTextBody{real}
}

func (ctx *Context) HTML(html Any, args ...Any) {
	ctx.clearBody()
	ctx.codingTyping("html", args...)

	if vv, ok := html.(string); ok {
		ctx.Body = httpHtmlBody{vv}
	} else {
		ctx.Body = httpHtmlBody{fmt.Sprintf("%v", html)}
	}
}

func (ctx *Context) JSON(json Any, args ...Any) {
	ctx.clearBody()
	ctx.codingTyping("json", args...)
	ctx.Body = httpJsonBody{json}
}

func (ctx *Context) JSONP(callback string, json Any, args ...Any) {
	ctx.clearBody()
	ctx.codingTyping("jsonp", args...)
	ctx.Body = httpJsonpBody{json, callback}
}

func (ctx *Context) File(file string, args ...string) {
	ctx.clearBody()
	name := ctx.fileTyping(args...)
	ctx.Body = httpFileBody{file, name}
}

func (ctx *Context) Binary(bytes []byte, args ...string) {
	ctx.clearBody()
	name := ctx.fileTyping(args...)
	ctx.Body = httpBinaryBody{bytes, name}
}

func (ctx *Context) Stream(buffer io.ReadCloser, size int64, args ...string) {
	ctx.clearBody()
	name := ctx.fileTyping(args...)
	ctx.Body = httpBufferBody{buffer, size, name}
}

func (ctx *Context) Proxy(target string) {
	ctx.clearBody()
	ctx.Body = httpProxyBody{target: target}
}

// View renders template by view module.
// args can include: int(status code), string(mime), Map(model).
func (ctx *Context) View(view string, args ...Any) {
	ctx.clearBody()

	code := 0
	mime := ""
	var model Map
	for _, arg := range args {
		switch vv := arg.(type) {
		case int:
			code = vv
		case string:
			mime = vv
		case Map:
			model = vv
		}
	}

	if code > 0 {
		ctx.Code = code
	}
	if mime != "" {
		ctx.Type = mime
	}
	if ctx.Type == "" {
		ctx.Type = "html"
	}

	ctx.Body = httpViewBody{view: view, model: model}
}

func (ctx *Context) fileTyping(args ...string) string {
	var mime, name string
	for _, arg := range args {
		if strings.Contains(arg, "/") {
			mime = arg
		} else if strings.Contains(arg, ".") {
			name = arg
		} else {
			mime = arg
		}
	}
	if mime != "" {
		ctx.Type = mime
	}
	return name
}

func (ctx *Context) Status(code int, texts ...string) {
	ctx.clearBody()
	ctx.Code = code
	if len(texts) > 0 {
		ctx.Body = httpStatusBody(texts[0])
	}
}

// Answer outputs API response.
func (ctx *Context) Answer(res Res, args ...Any) {
	ctx.clearBody()

	code := 0
	text := ""
	if res != nil {
		code = res.Code()
		text = ctx.String(res.Status(), res.Args()...)
	}

	if res == nil || res.OK() {
		ctx.Code = StatusOK
	} else {
		if ctx.Code <= 0 {
			ctx.Code = StatusInternalServerError
		}
	}

	var data Map
	if len(ctx.Data) > 0 {
		data = make(Map)
		for k, v := range ctx.Data {
			data[k] = v
		}
	}

	for _, arg := range args {
		if vvs, ok := arg.(Map); ok {
			if data == nil {
				data = make(Map)
			}
			for k, v := range vvs {
				data[k] = v
			}
		}
	}

	for k, v := range data {
		ctx.Data[k] = v
	}

	ctx.Type = "json"
	ctx.Body = httpAnswerBody{code, text, data}
}

func (ctx *Context) uploadFile(patterns ...string) (*os.File, error) {
	pattern := ""
	if len(patterns) > 0 {
		pattern = patterns[0]
	}
	dir := ctx.site.Config.Upload
	if dir == "" {
		dir = os.TempDir()
	}
	file, err := os.CreateTemp(dir, pattern)
	if err == nil {
		ctx.uploadfiles = append(ctx.uploadfiles, file.Name())
	}
	return file, err
}
