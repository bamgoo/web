package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

var (
	resLoadEmpty = infra.Result(-1, "web.load.empty", "empty load.")
	resLoadError = infra.Result(-1, "web.load.error", "error load.")
)

// preprocessing handles token and language.
func (site *webSite) preprocessing(ctx *Context) {
	if site.Config.MaxBodyBytes > 0 && ctx.reader.Body != nil {
		ctx.reader.Body = http.MaxBytesReader(ctx.writer, ctx.reader.Body, site.Config.MaxBodyBytes)
	}
	token := ""
	if ctx.site.Config.Cookie != "" {
		if c, e := ctx.reader.Cookie(ctx.site.Config.Cookie); e == nil {
			token = strings.TrimSpace(c.Value)
		}
	}
	if vv := extractBearerToken(ctx.Header("Authorization")); vv != "" {
		token = vv
	}
	if vv := extractBearerToken(ctx.Header("X-Forwarded-Access-Token")); vv != "" {
		token = vv
	}

	if token != "" {
		ctx.Verify(token)
	}
	if tp := ctx.Header("traceparent"); tp != "" {
		ctx.ParseTraceParent(tp)
	}

	// Detect AJAX
	if ctx.Header("X-Requested-With") != "" ||
		ctx.Header("Authorization") != "" ||
		ctx.Header("Ajax") != "" {
		ctx.Ajax = true
	}

	// Language from Accept-Language
	if al := ctx.Header("Accept-Language"); al != "" {
		accepts := strings.Split(al, ",")
		if len(accepts) > 0 {
			for _, accept := range accepts {
				if i := strings.Index(accept, ";"); i > 0 {
					accept = accept[0:i]
				}
				for lang, config := range infra.Languages() {
					for _, acccc := range config.Accepts {
						if strings.EqualFold(acccc, accept) {
							ctx.Language(lang)
							break
						}
					}
				}
			}
		}
	}

	ctx.Next()
}

func extractBearerToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) >= 7 && strings.EqualFold(v[0:7], "Bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return v
}

// finding handles static files.
func (site *webSite) finding(ctx *Context) {
	if ctx.Name == "" {
		fsys := infra.AssetFS()
		staticRoot := ctx.site.Config.Static
		if staticRoot == "" {
			staticRoot = "asset/statics"
		}
		file := resolveStaticFile(staticRoot, ctx.Path, ctx.site.Config.Defaults, fsys)
		if file == "" {
			sharedStaticRoot := module.config.Static
			if sharedStaticRoot == "" {
				sharedStaticRoot = "asset/statics"
			}
			sharedDir := module.config.Shared
			if sharedDir == "" {
				sharedDir = "shared"
			}

			defaults := module.config.Defaults
			if len(defaults) == 0 {
				defaults = ctx.site.Config.Defaults
			}

			sharedRoot := path.Join(sharedStaticRoot, sharedDir)
			file = resolveStaticFile(sharedRoot, ctx.Path, defaults, fsys)
		}

		if file != "" && !strings.Contains(file, "../") {
			if fsys != nil {
				bts, err := infra.AssetFile(file)
				if err == nil {
					ext := path.Ext(file)
					if strings.HasPrefix(ext, ".") {
						ext = ext[1:]
					}
					ctx.Binary(bts, infra.Mimetype(ext, "application/octet-stream"))
					return
				}
			}
			ctx.File(file)
		} else {
			ctx.NotFound()
		}
		return
	}

	ctx.Next()
}

// crossing handles CORS.
func (site *webSite) crossing(ctx *Context) {
	cross := ctx.site.Cross
	if !cross.Allow {
		ctx.Next()
		return
	}

	allowOrigins := mergeAllow(cross.Origin, cross.Origins)
	allowMethods := mergeAllow(cross.Method, cross.Methods)
	allowHeaders := mergeAllow(cross.Header, cross.Headers)

	origin := ctx.Header("Origin")
	method := ctx.Header("Access-Control-Request-Method")
	header := ctx.Header("Access-Control-Request-Headers")

	originPassed := originAllowed(origin, allowOrigins)
	methodPassed := valuesAllowed(splitCSV(method), allowMethods)
	headerPassed := valuesAllowed(splitCSV(header), allowHeaders)

	if originPassed && methodPassed && headerPassed {
		ctx.Header("Access-Control-Allow-Credentials", "true")
		if origin != "" {
			ctx.Header("Access-Control-Allow-Origin", origin)
		}
		if method != "" {
			ctx.Header("Access-Control-Allow-Methods", method)
		}
		if header != "" {
			ctx.Header("Access-Control-Allow-Headers", header)
			ctx.Header("Access-Control-Expose-Headers", header)
		}

		if strings.EqualFold(ctx.Method, OPTIONS) {
			ctx.Text("cross domain access allowed.", http.StatusOK)
			return
		}
	}

	ctx.Next()
}

func resolveStaticFile(root, requestPath string, defaults []string, fsys fs.FS) string {
	if root == "" {
		root = "asset/statics"
	}
	cleanPath := path.Clean("/" + requestPath)
	target := path.Join(root, cleanPath)

	if fsys != nil {
		if fi, err := fs.Stat(fsys, target); err == nil {
			if fi.IsDir() {
				for _, doc := range defaults {
					docPath := path.Join(target, doc)
					if ff, err := fs.Stat(fsys, docPath); err == nil && !ff.IsDir() {
						return docPath
					}
				}
				return ""
			}
			return target
		}
		// fallback to local filesystem
	}

	fi, err := os.Stat(target)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		for _, doc := range defaults {
			docPath := path.Join(target, doc)
			if ff, err := os.Stat(docPath); err == nil && !ff.IsDir() {
				return docPath
			}
		}
		return ""
	}
	return target
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	items := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			items = append(items, p)
		}
	}
	return items
}

func containsAll(got []string, allow []string) bool {
	if len(got) == 0 {
		return true
	}
	set := map[string]struct{}{}
	for _, v := range allow {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			set[v] = struct{}{}
		}
	}
	for _, v := range got {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

func containsOrigin(origins []string, origin string) bool {
	origin = normalizeOrigin(origin)
	if origin == "" {
		return false
	}
	originScheme, originHost, ok := parseOrigin(origin)
	if !ok {
		return false
	}

	for _, item := range origins {
		pattern := normalizeOrigin(item)
		if pattern == "" {
			continue
		}
		if pattern == "*" || pattern == origin {
			return true
		}

		patternScheme, patternHost, parsed := parseOrigin(pattern)
		if parsed {
			if strings.HasPrefix(patternHost, "*.") {
				base := strings.TrimPrefix(patternHost, "*.")
				if patternScheme == originScheme && wildcardHostMatch(originHost, base) {
					return true
				}
			}
			continue
		}

		if strings.HasPrefix(pattern, "*.") {
			base := strings.TrimPrefix(pattern, "*.")
			if wildcardHostMatch(originHost, base) {
				return true
			}
			continue
		}

		if originHost == normalizeHost(pattern) {
			return true
		}
	}
	return false
}

func containsString(vals []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, v := range vals {
		if strings.ToLower(strings.TrimSpace(v)) == target {
			return true
		}
	}
	return false
}

func mergeAllow(primary string, extras []string) []string {
	out := make([]string, 0, len(extras)+1)
	if primary != "" {
		out = append(out, primary)
	}
	out = append(out, extras...)
	return out
}

func valuesAllowed(requested []string, allow []string) bool {
	if len(requested) == 0 {
		return true
	}
	if len(allow) == 0 || containsString(allow, "*") {
		return true
	}
	return containsAll(requested, allow)
}

func originAllowed(origin string, allow []string) bool {
	origin = normalizeOrigin(origin)
	if origin == "" {
		return true
	}
	if len(allow) == 0 || containsString(allow, "*") {
		return true
	}
	return containsOrigin(allow, origin)
}

func normalizeOrigin(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	return strings.TrimSuffix(v, "/")
}

func parseOrigin(v string) (scheme, host string, ok bool) {
	u, err := url.Parse(v)
	if err != nil || u == nil {
		return "", "", false
	}
	if u.Scheme == "" || u.Host == "" {
		return "", "", false
	}

	host = normalizeHost(u.Host)
	if host == "" {
		return "", "", false
	}

	return strings.ToLower(u.Scheme), host, true
}

func wildcardHostMatch(host, base string) bool {
	host = normalizeHost(host)
	base = normalizeHost(base)
	if host == "" || base == "" || host == base {
		return false
	}
	return strings.HasSuffix(host, "."+base)
}

// authorizing handles authentication.
func (site *webSite) authorizing(ctx *Context) {
	if ctx.Config.Sign {
		if !ctx.Signed() {
			ctx.Unsign()
			return
		}
	}

	if ctx.Config.Auth {
		if !ctx.Authed() {
			ctx.Unauth()
			return
		}
	}

	ctx.Next()
}

// parsing parses request body.
func (site *webSite) parsing(ctx *Context) {
	req := ctx.reader

	// URL params
	for key, val := range ctx.Params {
		if vv, ok := val.(string); ok {
			ctx.Value[key] = vv
		} else if vs, ok := val.([]string); ok && len(vs) > 0 {
			if len(vs) == 1 {
				ctx.Value[key] = vs[0]
			} else {
				ctx.Value[key] = vs
			}
		} else {
			ctx.Value[key] = fmt.Sprintf("%v", val)
		}
	}

	// URL query
	for key, vals := range req.URL.Query() {
		if len(vals) == 1 {
			ctx.Query[key] = vals[0]
			ctx.Value[key] = vals[0]
		} else if len(vals) > 1 {
			ctx.Query[key] = vals
			ctx.Value[key] = vals
		}
	}

	if !strings.EqualFold(ctx.Method, GET) {
		ctype := ctx.Header("Content-Type")

		if strings.Contains(ctype, "json") {
			body, err := io.ReadAll(req.Body)
			if err == nil {
				ctx.RequestBody = append(ctx.RequestBody[:0], body...)
				var jsonBody Map
				if err := json.Unmarshal(body, &jsonBody); err == nil {
					for key, val := range jsonBody {
						ctx.Form[key] = val
						ctx.Value[key] = val
					}
				}
			}
		} else {
			if !strings.Contains(strings.ToLower(ctype), "multipart/") {
				body, err := io.ReadAll(req.Body)
				if err == nil {
					ctx.RequestBody = append(ctx.RequestBody[:0], body...)
					req.Body = io.NopCloser(bytes.NewReader(body))
				}
			}
			// Parse form
			err := req.ParseMultipartForm(32 << 20)
			if err != nil {
				body, err := io.ReadAll(req.Body)
				if err == nil {
					ctx.Body = string(body)
				}
			}

			if req.MultipartForm != nil {
				for key, vals := range req.MultipartForm.Value {
					if len(vals) == 1 {
						ctx.Form[key] = vals[0]
						ctx.Value[key] = vals[0]
					} else if len(vals) > 1 {
						ctx.Form[key] = vals
						ctx.Value[key] = vals
					}
				}

				// Handle file uploads
				for key, vs := range req.MultipartForm.File {
					files := []Map{}
					for _, f := range vs {
						if f.Size <= 0 || f.Filename == "" {
							continue
						}

						file, err := f.Open()
						if err != nil {
							continue
						}

						ext := ""
						if idx := strings.LastIndex(f.Filename, "."); idx > 0 {
							ext = f.Filename[idx+1:]
						}

						tempfile, err := ctx.uploadFile("upload_*." + ext)
						if err != nil {
							file.Close()
							continue
						}

						io.Copy(tempfile, file)
						tempfile.Close()
						file.Close()

						files = append(files, Map{
							"name": f.Filename,
							"type": ext,
							"mime": f.Header.Get("Content-Type"),
							"size": f.Size,
							"file": tempfile.Name(),
						})
					}

					if len(files) == 1 {
						ctx.Upload[key] = files[0]
						ctx.Value[key] = files[0]
					} else if len(files) > 1 {
						ctx.Upload[key] = files
						ctx.Value[key] = files
					}
				}
			} else if req.PostForm != nil {
				for key, vals := range req.PostForm {
					if len(vals) == 1 {
						ctx.Form[key] = vals[0]
						ctx.Value[key] = vals[0]
					} else if len(vals) > 1 {
						ctx.Form[key] = vals
						ctx.Value[key] = vals
					}
				}
			}
		}
	}

	ctx.Next()
}

// arguing validates and maps arguments.
func (site *webSite) arguing(ctx *Context) {
	if ctx.Config.Args != nil {
		argsValue := Map{}
		res := infra.Mapping(ctx.Config.Args, ctx.Value, argsValue, ctx.Config.Nullable, false, ctx.Timezone())
		if res != nil && res.Fail() {
			ctx.Fail(res)
			return
		}
		for k, v := range argsValue {
			ctx.Args[k] = v
		}
	}
	ctx.Next()
}

func (site *webSite) loading(ctx *Context) {
	if len(ctx.Config.Loading) == 0 {
		ctx.Next()
		return
	}

	for key, loader := range ctx.Config.Loading {
		valueKey := strings.TrimSpace(loader.Value)
		if valueKey == "" {
			valueKey = "id"
		}

		value, ok := ctx.Args[valueKey]
		if !ok {
			value, ok = ctx.Value[valueKey]
		}

		if value == nil && loader.Required {
			if loader.Empty != nil {
				ctx.Fail(loader.Empty)
			} else {
				ctx.Fail(resLoadEmpty)
			}
			return
		}

		target := strings.TrimSpace(loader.Invoke)
		if target == "" || value == nil {
			continue
		}

		argsKey := strings.TrimSpace(loader.Args)
		if argsKey == "" {
			argsKey = "id"
		}

		item := ctx.Invoke(target, Map{argsKey: value})
		if (item == nil || (ctx.Result() != nil && ctx.Result().Fail())) && loader.Required {
			if loader.Error != nil {
				ctx.Fail(loader.Error)
			} else {
				ctx.Fail(resLoadError)
			}
			return
		}

		ctx.Locals[key] = item
	}

	ctx.Next()
}
