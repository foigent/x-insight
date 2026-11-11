package webui_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/webui"
)

func TestImmutableAssetGzipNegotiationAndOriginalBytes(t *testing.T) {
	handler := webui.Handler()
	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/login", nil))
	for _, route := range assetReferencePattern.FindAllString(index.Body.String(), -1) {
		original := httptest.NewRecorder()
		handler.ServeHTTP(original, httptest.NewRequest(http.MethodGet, route, nil))
		for _, encoding := range []string{"gzip", "br, gzip, deflate", "GZIP;q=0.5", "*;q=0.8"} {
			t.Run(route+encoding, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, route, nil)
				request.Header.Set("Accept-Encoding", encoding)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "gzip" || response.Header().Get("Vary") != "Accept-Encoding" {
					t.Fatalf("negotiation status%d headers%v", response.Code, response.Header())
				}
				reader, err := gzip.NewReader(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := io.ReadAll(reader)
				reader.Close()
				if err != nil || !bytes.Equal(decoded, original.Body.Bytes()) {
					t.Fatal("compressed representation differs from the manifest-verified asset", err)
				}
				size, err := strconv.Atoi(response.Header().Get("Content-Length"))
				if err != nil || size >= original.Body.Len() || response.Header().Get("Content-Type") != original.Header().Get("Content-Type") {
					t.Fatalf("representation size%d original%d type%q", size, original.Body.Len(), response.Header().Get("Content-Type"))
				}
				request.Method = http.MethodHead
				head := httptest.NewRecorder()
				handler.ServeHTTP(head, request)
				if head.Body.Len() != 0 || head.Header().Get("Content-Length") != response.Header().Get("Content-Length") || head.Header().Get("Content-Encoding") != "gzip" {
					t.Fatal("HEAD does not describe the GET representation")
				}
			})
		}
		for _, encoding := range []string{"", "br", "gzip;q=0", "gzip;q=0, *;q=1", "*;q=0", "gzip;q=invalid", "gzip;q=2", "gzip;q=NaN"} {
			request := httptest.NewRequest(http.MethodGet, route, nil)
			request.Header.Set("Accept-Encoding", encoding)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Header().Get("Content-Encoding") != "" || !bytes.Equal(response.Body.Bytes(), original.Body.Bytes()) || response.Header().Get("Vary") != "Accept-Encoding" {
				t.Fatalf("identity representation changed for %q", encoding)
			}
		}
		request := httptest.NewRequest(http.MethodGet, route, nil)
		request.Header.Set("Accept-Encoding", "gzip")
		request.Header.Set("Range", "bytes=0-15")
		partial := httptest.NewRecorder()
		handler.ServeHTTP(partial, request)
		if partial.Code != http.StatusPartialContent || partial.Header().Get("Content-Encoding") != "" || !bytes.Equal(partial.Body.Bytes(), original.Body.Bytes()[:16]) {
			t.Fatal("original asset range behavior changed")
		}
	}
}

var assetReferencePattern = regexp.MustCompile(`/assets/[A-Za-z0-9][A-Za-z0-9._-]*`)

func TestProductRoutesServeEmbeddedEnglishShell(t *testing.T) {
	for _, route := range []string{
		"/", "/inbox", "/workbench/runtime", "/workbench/runtime?view=capabilities", "/executions",
		"/executions/xe1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"/investigation/queue", "/investigation/queue?mode=created",
		"/cases", "/cases/xic_0123456789abcdefghijkl", "/sessions",
		"/sessions/session-1", "/compare", "/connectors", "/connectors/unirouter/setup",
		"/connectors/unirouter/setup?installation=xci_AAAAAAAAAAAAAAAAAAAAAA", "/discovery",
		"/access/accounts", "/access/activate", "/access/sessions", "/access/review-policy", "/work/activity", "/access/service-identities",
	} {
		t.Run(route, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, route, nil)
			webui.Handler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `<html lang="en-US">`) {
				t.Fatalf("expected en-US shell, body=%s", response.Body.String())
			}
			if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
				t.Fatalf("missing CSP: %q", got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("cache-control=%q", got)
			}
			if got := response.Header().Get("Permissions-Policy"); got == "" {
				t.Fatal("missing Permissions-Policy")
			}
		})
	}
}

func TestHandlerDeclaresAllowedMethods(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/executions", nil)
	webui.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("allow=%q", got)
	}
}

func TestWorkbenchDocumentRoutes(t *testing.T) {
	for _, route := range []string{"/workbench/cost", "/workbench/cost?view=usage", "/workbench/runtime", "/workbench/runtime?view=capabilities", "/connectors/unirouter/setup", "/connectors/unirouter/setup/", "/connectors/unirouter/setup?installation=xci_AAAAAAAAAAAAAAAAAAAAAA", "/access/review-policy", "/work/activity"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			t.Run(method+route, func(t *testing.T) {
				response := httptest.NewRecorder()
				webui.Handler().ServeHTTP(response, httptest.NewRequest(method, route, nil))
				if method == http.MethodPost {
					if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
						t.Fatalf("status=%d allow=%q", response.Code, response.Header().Get("Allow"))
					}
					return
				}
				if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Header().Get("Content-Type"), "text/html") {
					t.Fatalf("status=%d headers=%v", response.Code, response.Header())
				}
				if method == http.MethodHead && response.Body.Len() != 0 {
					t.Fatal("HEAD returned a body")
				}
				if method == http.MethodGet && !strings.Contains(response.Body.String(), `<html lang="en-US">`) {
					t.Fatal("GET did not return the product shell")
				}
			})
		}
	}
}

func TestHandlerNeverTurnsAPIOrUnknownRoutesIntoSPA(t *testing.T) {
	for _, route := range []string{"/api/v1/executions", "/api/v1/workbench/cost", "/healthz", "/unknown", "/workbench", "/workbench/unknown", "/workbench/cost/unknown", "/connectors/unirouter", "/connectors/unirouter/setup/unknown", "/connectors/unknown", "/access/accounts/unknown", "/access/activate/unknown", "/access/review-policy/unknown", "/work/activity/unknown"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, route, nil)
		webui.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("route=%s status=%d body=%s", route, response.Code, response.Body.String())
		}
	}
}

func TestHandlerOnlyServesAssetsReferencedByCurrentIndex(t *testing.T) {
	handler := webui.Handler()
	indexResponse := httptest.NewRecorder()
	indexRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(indexResponse, indexRequest)
	if indexResponse.Code != http.StatusOK {
		t.Fatalf("index status=%d body=%s", indexResponse.Code, indexResponse.Body.String())
	}

	references := assetReferencePattern.FindAllString(indexResponse.Body.String(), -1)
	if len(references) != 2 {
		t.Fatalf("asset references=%v", references)
	}
	for _, reference := range references {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, reference, nil)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("current asset=%s status=%d body=%s", reference, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Fatalf("current asset=%s cache-control=%q", reference, got)
		}
	}

	for _, stale := range []string{
		"/assets/index-BibMuBXx.js",
		"/assets/index-pndZqYsI.css",
	} {
		if strings.Contains(indexResponse.Body.String(), stale) {
			t.Fatalf("stale asset is referenced by current index: %s", stale)
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, stale, nil)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("stale asset=%s status=%d body=%s", stale, response.Code, response.Body.String())
		}
	}
}

func TestEmbeddedManifestMatchesEveryServedReleaseByte(t *testing.T) {
	manifestBytes, err := webui.EmbeddedAssetManifest()
	if err != nil {
		t.Fatal(err)
	}
	var manifest webui.AssetManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != webui.AssetManifestSchema || len(manifest.Entries) != 3 {
		t.Fatalf("manifest=%+v", manifest)
	}

	handler := webui.Handler()
	for _, entry := range manifest.Entries {
		route := "/" + entry.Path
		if entry.Path == "index.html" {
			route = "/"
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("path=%s status=%d body=%s", entry.Path, response.Code, response.Body.String())
		}
		if int64(response.Body.Len()) != entry.SizeBytes {
			t.Fatalf("path=%s size=%d expected=%d", entry.Path, response.Body.Len(), entry.SizeBytes)
		}
		digest := sha256.Sum256(response.Body.Bytes())
		if got := "sha256:" + hex.EncodeToString(digest[:]); got != entry.SHA256 {
			t.Fatalf("path=%s digest=%s expected=%s", entry.Path, got, entry.SHA256)
		}
	}
}

func TestEmbeddedManualIsReleaseMatchedAndHasNoExternalDependency(t *testing.T) {
	for _, language := range []string{"en-US", "zh-CN"} {
		t.Run(language, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/manual/"+language+"/", nil)
			webui.Handler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			body := response.Body.String()
			var config struct {
				ProductVersion string `json:"product_version"`
				Manual         struct {
					Version string `json:"version"`
				} `json:"manual"`
			}
			raw, err := os.ReadFile("../../release/d-validation-config.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &config); err != nil || config.ProductVersion == "" || config.Manual.Version == "" {
				t.Fatal("candidate manual identity", err)
			}
			for _, expected := range []string{
				`<html lang="` + language + `">`,
				`name="xinsight-product-version" content="` + config.ProductVersion + `"`,
				`name="xinsight-manual-version" content="` + config.Manual.Version + `"`,
				`href="manual-icon.svg"`,
				`href="manual.css"`,
			} {
				if !strings.Contains(body, expected) {
					t.Fatalf("missing %q", expected)
				}
			}
			for _, forbidden := range []string{`href="http:`, `href="https:`, `src="http:`, `src="https:`} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("external dependency %q", forbidden)
				}
			}

			cssResponse := httptest.NewRecorder()
			cssRequest := httptest.NewRequest(http.MethodHead, "/manual/"+language+"/manual.css", nil)
			webui.Handler().ServeHTTP(cssResponse, cssRequest)
			if cssResponse.Code != http.StatusOK || cssResponse.Header().Get("Content-Type") != "text/css; charset=utf-8" || cssResponse.Body.Len() != 0 {
				t.Fatalf("css status=%d type=%q body=%q", cssResponse.Code, cssResponse.Header().Get("Content-Type"), cssResponse.Body.String())
			}
		})
	}
}
