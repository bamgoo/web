package web

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/bamgoo/bamgoo"
	. "github.com/bamgoo/base"
)

type (
	Context struct {
		site *Site
		*bamgoo.Meta

		uploadfiles []string

		index int
		nexts []ctxFunc

		reader *http.Request
		writer http.ResponseWriter

		Name    string
		Config  Router
		Setting Map

		charset string
		headers map[string]string
		cookies map[string]http.Cookie

		Method string
		Host   string
		Path   string
		Uri    string

		Ajax bool

		Params Map
		Query  Map
		Form   Map
		Upload Map

		Value  Map
		Args   Map
		Locals Map

		Code int
		Type string
		Data Map
		Body Any
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

func (ctx *Context) Found() {
	ctx.site.found(ctx)
}

func (ctx *Context) Error(res Res) {
	ctx.Result(res)
	ctx.site.error(ctx)
}

func (ctx *Context) Failed(res Res) {
	ctx.Result(res)
	ctx.site.failed(ctx)
}

func (ctx *Context) Denied(res Res) {
	ctx.Result(res)
	ctx.site.denied(ctx)
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
			cookie := http.Cookie{Name: key, HttpOnly: true, MaxAge: -1}
			ctx.cookies[key] = cookie
			return ""
		}
		switch val := vvv.(type) {
		case http.Cookie:
			ctx.cookies[key] = val
		case string:
			cookie := http.Cookie{Name: key, Value: val}
			ctx.cookies[key] = cookie
		}
		return ""
	}

	c, err := ctx.reader.Cookie(key)
	if err == nil {
		return c.Value
	}
	return ""
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
		real = ctx.String(res.State(), res.Args()...)
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

func (ctx *Context) Buffer(buffer io.ReadCloser, size int64, args ...string) {
	ctx.clearBody()
	name := ctx.fileTyping(args...)
	ctx.Body = httpBufferBody{buffer, size, name}
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

// Echo outputs API response.
func (ctx *Context) Echo(res Res, args ...Any) {
	ctx.clearBody()

	code := 0
	text := ""
	if res != nil {
		code = res.Code()
		text = ctx.String(res.State(), res.Args()...)
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
	ctx.Body = httpEchoBody{code, text, data}
}

func (ctx *Context) uploadFile(patterns ...string) (*os.File, error) {
	if dir := ctx.site.Config.Upload; dir != "" {
		pattern := ""
		if len(patterns) > 0 {
			pattern = patterns[0]
		}
		file, err := os.CreateTemp(dir, pattern)
		if err == nil {
			ctx.uploadfiles = append(ctx.uploadfiles, file.Name())
		}
		return file, err
	}
	return ctx.TempFile(patterns...)
}
