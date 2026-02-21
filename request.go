package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/bamgoo/bamgoo"
	. "github.com/bamgoo/base"
)

// preprocessing handles token and language.
func (site *Site) preprocessing(ctx *Context) {
	token := ""
	if ctx.site.Config.Cookie != "" {
		if c, e := ctx.reader.Cookie(ctx.site.Config.Cookie); e == nil {
			token = c.Value
		}
	}
	if vv := ctx.Header("Authorization"); vv != "" {
		token = strings.TrimPrefix(vv, "Bearer ")
	}

	if token != "" {
		ctx.Verify(token)
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
				for lang, config := range bamgoo.Languages() {
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

// finding handles static files.
func (site *Site) finding(ctx *Context) {
	if ctx.Name == "" {
		file := resolveStaticFile(ctx.site.Config.Static, ctx.Path, ctx.site.Config.Defaults)
		if file == "" && module.config.Static != "" && module.config.Shared != "" {
			sharedRoot := path.Join(module.config.Static, module.config.Shared)
			file = resolveStaticFile(sharedRoot, ctx.Path, module.config.Defaults)
		}

		if file != "" && !strings.Contains(file, "../") {
			ctx.File(file)
		} else {
			ctx.Found()
		}
		return
	}

	ctx.Next()
}

// crossing handles CORS.
func (site *Site) crossing(ctx *Context) {
	cross := ctx.site.Cross

	if cross.Allow {
		origin := ctx.Header("Origin")
		originPassed := false

		if cross.Origin == "*" || cross.Origin == "" || containsString(cross.Origins, "*") {
			originPassed = true
		} else if origin != "" && containsOrigin(cross.Origins, origin) {
			originPassed = true
		}

		method := ctx.Header("Access-Control-Request-Method")
		methodPassed := cross.Method == "*" || cross.Method == "" || containsString(cross.Methods, "*")
		if !methodPassed {
			methodPassed = method == "" || containsAll(splitCSV(method), cross.Methods)
		}

		header := ctx.Header("Access-Control-Request-Headers")
		headerPassed := cross.Header == "*" || cross.Header == "" || containsString(cross.Headers, "*")
		if !headerPassed {
			headerPassed = header == "" || containsAll(splitCSV(header), cross.Headers)
		}

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

			if ctx.Method == OPTIONS {
				ctx.Text("cross domain access allowed.", http.StatusOK)
				return
			}
		}
	}

	ctx.Next()
}

func resolveStaticFile(root, requestPath string, defaults []string) string {
	if root == "" {
		return ""
	}
	cleanPath := path.Clean("/" + requestPath)
	target := path.Join(root, cleanPath)
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
	origin = strings.ToLower(strings.TrimSpace(origin))
	for _, item := range origins {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if origin == item || strings.HasPrefix(origin, item) {
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

// authorizing handles authentication.
func (site *Site) authorizing(ctx *Context) {
	if ctx.Config.Sign {
		if !ctx.Signed() {
			ctx.Result(bamgoo.Unsigned)
			site.denied(ctx)
			return
		}
	}

	if ctx.Config.Auth {
		if !ctx.Authed() {
			ctx.Result(bamgoo.Unauthed)
			site.denied(ctx)
			return
		}
	}

	ctx.Next()
}

// parsing parses request body.
func (site *Site) parsing(ctx *Context) {
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

	if ctx.Method != "GET" {
		ctype := ctx.Header("Content-Type")

		if strings.Contains(ctype, "json") {
			body, err := io.ReadAll(req.Body)
			if err == nil {
				var jsonBody Map
				if err := json.Unmarshal(body, &jsonBody); err == nil {
					for key, val := range jsonBody {
						ctx.Form[key] = val
						ctx.Value[key] = val
					}
				}
			}
		} else {
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
func (site *Site) arguing(ctx *Context) {
	if ctx.Config.Args != nil {
		argsValue := Map{}
		res := bamgoo.Mapping(ctx.Config.Args, ctx.Value, argsValue, ctx.Config.Nullable, false, ctx.Timezone())
		if res != nil && res.Fail() {
			ctx.Failed(res)
			return
		}
		for k, v := range argsValue {
			ctx.Args[k] = v
		}
	}
	ctx.Next()
}
