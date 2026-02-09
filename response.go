package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/bamgoo/bamgoo"
	. "github.com/bamgoo/base"
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
	httpEchoBody struct {
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
	httpStatusBody string
)

func (site *Site) body(ctx *Context) {
	if ctx.Code <= 0 {
		ctx.Code = StatusOK
	}

	// Write headers
	for k, v := range ctx.headers {
		ctx.writer.Header().Set(k, v)
	}

	// Write cookies
	for _, cookie := range ctx.cookies {
		cookie.Path = "/"
		cookie.HttpOnly = ctx.site.Config.HttpOnly
		if ctx.site.Config.MaxAge > 0 {
			cookie.MaxAge = int(ctx.site.Config.MaxAge.Seconds())
		}
		http.SetCookie(ctx.writer, &cookie)
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
	case httpEchoBody:
		site.bodyEcho(ctx, body)
	case httpFileBody:
		site.bodyFile(ctx, body)
	case httpBinaryBody:
		site.bodyBinary(ctx, body)
	case httpBufferBody:
		site.bodyBuffer(ctx, body)
	case httpStatusBody:
		site.bodyStatus(ctx, body)
	default:
		site.bodyDefault(ctx)
	}
}

func (site *Site) bodyDefault(ctx *Context) {
	if ctx.Code <= 0 {
		ctx.Code = StatusNotFound
		http.NotFound(ctx.writer, ctx.reader)
	} else {
		ctx.writer.WriteHeader(ctx.Code)
		fmt.Fprint(ctx.writer, StatusText(ctx.Code))
	}
}

func (site *Site) bodyStatus(ctx *Context, body httpStatusBody) {
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

func (site *Site) bodyGoto(ctx *Context, body httpGotoBody) {
	http.Redirect(ctx.writer, ctx.reader, body.url, StatusFound)
}

func (site *Site) bodyText(ctx *Context, body httpTextBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "text"
	}

	mimeType := bamgoo.Mimetype(ctx.Type, "text/plain")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	res.WriteHeader(ctx.Code)
	fmt.Fprint(res, body.text)
}

func (site *Site) bodyHtml(ctx *Context, body httpHtmlBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "html"
	}

	mimeType := bamgoo.Mimetype(ctx.Type, "text/html")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	res.WriteHeader(ctx.Code)
	fmt.Fprint(res, body.html)
}

func (site *Site) bodyJson(ctx *Context, body httpJsonBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "json"
	}

	bytes, err := json.Marshal(body.json)
	if err != nil {
		http.Error(res, err.Error(), StatusInternalServerError)
		return
	}

	mimeType := bamgoo.Mimetype(ctx.Type, "application/json")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))
	res.WriteHeader(ctx.Code)
	fmt.Fprint(res, string(bytes))
}

func (site *Site) bodyJsonp(ctx *Context, body httpJsonpBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "script"
	}

	bytes, err := json.Marshal(body.json)
	if err != nil {
		http.Error(res, err.Error(), StatusInternalServerError)
		return
	}

	mimeType := bamgoo.Mimetype(ctx.Type, "application/javascript")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	res.WriteHeader(ctx.Code)
	fmt.Fprintf(res, "%s(%s);", body.callback, string(bytes))
}

func (site *Site) bodyEcho(ctx *Context, body httpEchoBody) {
	result := Map{
		"code": body.code,
		"time": time.Now().Unix(),
	}

	if body.text != "" {
		result["text"] = body.text
	}

	if body.data != nil {
		result["data"] = body.data
	}

	site.bodyJson(ctx, httpJsonBody{result})
}

func (site *Site) bodyFile(ctx *Context, body httpFileBody) {
	req, res := ctx.reader, ctx.writer

	if ctx.Type == "" {
		ctx.Type = "file"
	}

	mimeType := bamgoo.Mimetype(ctx.Type, "application/octet-stream")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	if body.name != "" {
		res.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%v;", url.QueryEscape(body.name)))
	}

	http.ServeFile(res, req, body.file)
}

func (site *Site) bodyBinary(ctx *Context, body httpBinaryBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "file"
	}

	mimeType := bamgoo.Mimetype(ctx.Type, "application/octet-stream")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	if body.name != "" {
		res.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%v;", url.QueryEscape(body.name)))
	}

	res.WriteHeader(ctx.Code)
	res.Write(body.bytes)
}

func (site *Site) bodyBuffer(ctx *Context, body httpBufferBody) {
	res := ctx.writer

	if ctx.Type == "" {
		ctx.Type = "file"
	}

	mimeType := bamgoo.Mimetype(ctx.Type, "application/octet-stream")
	res.Header().Set("Content-Type", fmt.Sprintf("%v; charset=%v", mimeType, ctx.Charset()))

	if body.name != "" {
		res.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%v;", url.QueryEscape(body.name)))
	}

	if body.size > 0 {
		res.Header().Set("Content-Length", fmt.Sprintf("%d", body.size))
	}

	res.WriteHeader(ctx.Code)
	io.Copy(res, body.buffer)
	body.buffer.Close()
}
