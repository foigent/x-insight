package webui

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed dist/* manual/zh-CN/* manual/en-US/*
var embedded embed.FS

const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

var manualRoutePattern = regexp.MustCompile(`^/manual/(en-US|zh-CN)(?:/(manual\.css|manual-icon\.svg))?/?$`)

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
	compressedAssets := make(map[string][]byte, len(allowedAssets))
	for name := range allowedAssets {
		contents, err := fs.ReadFile(assets, strings.TrimPrefix(name, "/"))
		if err != nil {
			panic("webui: validated static asset is unavailable")
		}
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(contents); err != nil {
			panic("webui: static asset compression failed")
		}
		if err := writer.Close(); err != nil {
			panic("webui: static asset compression failed")
		}
		compressedAssets[name] = compressed.Bytes()
	}
	manual, err := fs.Sub(embedded, "manual")
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
		case manualRoutePattern.MatchString(cleanPath):
			parts := manualRoutePattern.FindStringSubmatch(cleanPath)
			name, contentType := parts[2], "text/html; charset=utf-8"
			switch name {
			case "":
				name = "index.html"
			case "manual.css":
				contentType = "text/css; charset=utf-8"
			case "manual-icon.svg":
				contentType = "image/svg+xml"
			}
			w.Header().Set("Cache-Control", "no-store")
			serveEmbeddedFile(w, r, manual, parts[1]+"/"+name, contentType)
		case strings.HasPrefix(cleanPath, "/assets/"):
			if _, ok := allowedAssets[cleanPath]; !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Add("Vary", "Accept-Encoding")
			if r.Header.Get("Range") == "" && acceptsGzip(r.Header.Values("Accept-Encoding")) {
				contents := compressedAssets[cleanPath]
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(cleanPath)))
				w.Header().Set("Content-Length", strconv.Itoa(len(contents)))
				http.ServeContent(w, r, path.Base(cleanPath), time.Time{}, bytes.NewReader(contents))
				return
			}
			files.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func acceptsGzip(values []string) bool {
	wildcard := false
	for _, value := range values {
		for _, entry := range strings.Split(value, ",") {
			coding, parameters, err := mime.ParseMediaType(strings.TrimSpace(entry))
			if err != nil {
				continue
			}
			quality := 1.0
			if text, ok := parameters["q"]; ok {
				quality, err = strconv.ParseFloat(text, 64)
			}
			accepted := err == nil && quality > 0 && quality <= 1
			if coding == "gzip" {
				return accepted
			}
			if coding == "*" {
				wildcard = accepted
			}
		}
	}
	return wildcard
}

func isProductRoute(cleanPath string) bool {
	switch cleanPath {
	case "/", "/login", "/inbox", "/investigation/queue", "/workbench/cost", "/workbench/runtime", "/executions", "/cases", "/sessions",
		"/compare", "/connectors", "/connectors/unirouter/setup", "/discovery", "/access/sessions", "/access/review-policy", "/work/activity",
		"/access/service-identities", "/access/accounts", "/access/activate":
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
