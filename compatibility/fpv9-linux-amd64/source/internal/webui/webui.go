package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

//go:embed dist/* manual/zh-CN/*
var embedded embed.FS

const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

var assetReferencePattern = regexp.MustCompile(`/assets/[A-Za-z0-9][A-Za-z0-9._-]*`)

func Handler() http.Handler {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("webui: embedded dist directory is unavailable")
	}
	_, allowedAssets, err := loadAssetManifest(assets)
	if err != nil {
		panic("webui: embedded release asset parity failed: " + err.Error())
	}
	files := http.FileServer(http.FS(assets))
	manual, err := fs.Sub(embedded, "manual/zh-CN")
	if err != nil {
		panic("webui: embedded manual directory is unavailable")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method is not allowed", http.StatusMethodNotAllowed)
			return
		}

		cleanPath := path.Clean("/" + strings.TrimSpace(r.URL.Path))
		if strings.HasPrefix(cleanPath, "/api/") || cleanPath == "/healthz" || cleanPath == "/readyz" {
			http.NotFound(w, r)
			return
		}

		setSecurityHeaders(w)
		switch {
		case isProductRoute(cleanPath):
			w.Header().Set("Cache-Control", "no-store")
			serveIndex(w, r, assets)
		case cleanPath == "/manual/zh-CN", cleanPath == "/manual/zh-CN/":
			w.Header().Set("Cache-Control", "no-store")
			serveEmbeddedFile(w, r, manual, "index.html", "text/html; charset=utf-8")
		case cleanPath == "/manual/zh-CN/manual.css":
			w.Header().Set("Cache-Control", "no-store")
			serveEmbeddedFile(w, r, manual, "manual.css", "text/css; charset=utf-8")
		case cleanPath == "/manual/zh-CN/manual-icon.svg":
			w.Header().Set("Cache-Control", "no-store")
			serveEmbeddedFile(w, r, manual, "manual-icon.svg", "image/svg+xml")
		case strings.HasPrefix(cleanPath, "/assets/"):
			if _, ok := allowedAssets[cleanPath]; !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func isProductRoute(cleanPath string) bool {
	switch cleanPath {
	case "/", "/login", "/inbox", "/investigation/queue", "/workbench/cost", "/workbench/runtime", "/executions", "/cases", "/sessions",
		"/compare", "/connectors", "/discovery", "/access/sessions",
		"/access/service-identities":
		return true
	}
	return strings.HasPrefix(cleanPath, "/executions/") ||
		strings.HasPrefix(cleanPath, "/cases/") ||
		strings.HasPrefix(cleanPath, "/sessions/")
}

func serveEmbeddedFile(w http.ResponseWriter, r *http.Request, assets fs.FS, name string, contentType string) {
	contents, err := fs.ReadFile(assets, name)
	if err != nil {
		http.Error(w, "embedded product content is unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(contents)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "web application is unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(index)
	}
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}
