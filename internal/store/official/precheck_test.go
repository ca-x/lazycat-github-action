package official_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/store/official"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func TestPublisherOfficialPrecheckRejectsOfficialWarningBeforeProviderOrNetwork(t *testing.T) {
	path, digest := publishLPKWithManifest(t, "application:\n  subdomain: publish-demo\n  image: docker.io/library/nginx:latest\n", false)
	provider := &countingTokenProvider{token: "ci-token"}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: provider, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeInvalidManifest {
		t.Fatalf("err=%v", err)
	}
	if provider.calls != 0 || requests != 0 {
		t.Fatalf("provider calls=%d network requests=%d", provider.calls, requests)
	}
	if errText := err.Error(); errText == "" || containsAny(errText, path, "docker.io", "nginx") {
		t.Fatalf("error exposed manifest details: %q", errText)
	}
}

func TestPublisherOfficialPrecheckAllowsCompatibilityWarning(t *testing.T) {
	path, digest := publishLPKWithManifest(t, "application:\n  subdomain: publish-demo\nservices:\n  web:\n    image: registry.lazycat.cloud/demo/web:1.0.0\n    container_name: publish-demo-web\n", false)
	provider := &countingTokenProvider{token: "ci-token"}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: provider, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || provider.calls != 1 || requests != 3 {
		t.Fatalf("result=%#v provider calls=%d network requests=%d", result, provider.calls, requests)
	}
}

func TestPublisherOfficialPrecheckSanitizesOperationalFailureBeforeProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.lpk")
	provider := &countingTokenProvider{token: "ci-token"}

	_, err := (official.Publisher{}).Publish(context.Background(), official.Request{
		Provider: provider, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: strings.Repeat("a", 64), Changelog: "Release notes", Locales: []string{"en"},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeNotFound || toolkitError.Op != "store.official.precheck" {
		t.Fatalf("err=%v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls=%d", provider.calls)
	}
	if strings.Contains(err.Error(), path) {
		t.Fatalf("error exposed LPK path: %q", err)
	}
}

func TestPublisherOfficialPrecheckIgnoresIrrelevantPayloadAndSpecialEntries(t *testing.T) {
	path, digest := publishZIPLPK(t, map[string]zipTestEntry{
		"package.yml": {
			contents: []byte("package: cloud.lazycat.apps.publish-demo\nversion: 1.0.0\nname: Publish Demo\nlocales:\n  en:\n    name: Publish Demo\n"),
		},
		"manifest.yml": {
			contents: []byte("application:\n  subdomain: publish-demo\n  image: registry.lazycat.cloud/demo/app:1.0.0\n"),
		},
		"icon.png":               {contents: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}},
		"content/irrelevant.bin": {contents: bytes.Repeat([]byte{0}, 2<<20)},
		"content/dangling-link":  {contents: []byte("../../outside"), mode: os.ModeSymlink | 0o777},
	})
	provider := &countingTokenProvider{token: "ci-token"}
	server, requests := successfulPublishServer(t, digest)

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: provider, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || provider.calls != 1 || *requests != 3 {
		t.Fatalf("result=%#v provider calls=%d network requests=%d", result, provider.calls, *requests)
	}
}

func TestPublisherOfficialPrecheckRejectsArchiveLimitBeforeProviderOrNetwork(t *testing.T) {
	longName := "content/" + strings.Repeat("a", 2048)
	path, digest := publishZIPLPK(t, map[string]zipTestEntry{
		"package.yml":  {contents: []byte("package: cloud.lazycat.apps.publish-demo\nversion: 1.0.0\n")},
		"manifest.yml": {contents: []byte("application:\n  subdomain: publish-demo\n")},
		"icon.png":     {contents: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}},
		longName:       {contents: []byte("irrelevant")},
	})
	provider := &countingTokenProvider{token: "ci-token"}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()

	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: provider, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeInvalidArgument || toolkitError.Op != "store.official.precheck" {
		t.Fatalf("err=%v", err)
	}
	if provider.calls != 0 || requests != 0 {
		t.Fatalf("provider calls=%d network requests=%d", provider.calls, requests)
	}
	if strings.Contains(err.Error(), longName) {
		t.Fatalf("error exposed archive path: %q", err)
	}
}

func TestPublisherOfficialPrecheckUsesReferencedBlobExistenceWithoutCopyingPayload(t *testing.T) {
	digestHex := strings.Repeat("a", 64)
	path, digest := publishZIPLPK(t, map[string]zipTestEntry{
		"package.yml": {
			contents: []byte("package: cloud.lazycat.apps.publish-demo\nversion: 1.0.0\nname: Publish Demo\nlocales:\n  en:\n    name: Publish Demo\n"),
		},
		"manifest.yml": {
			contents: []byte("application:\n  subdomain: publish-demo\n  image: embed:web\n"),
		},
		"icon.png": {contents: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}},
		"images.lock": {
			contents: []byte("images:\n  web:\n    layers:\n      - digest: sha256:" + digestHex + "\n        source: embed\n"),
		},
		"images/blobs/sha256/" + digestHex: {contents: bytes.Repeat([]byte{0}, 2<<20)},
	})
	provider := &countingTokenProvider{token: "ci-token"}
	server, requests := successfulPublishServer(t, digest)

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: provider, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || provider.calls != 1 || *requests != 3 {
		t.Fatalf("result=%#v provider calls=%d network requests=%d", result, provider.calls, *requests)
	}
}

func TestPublisherOfficialPrecheckSupportsResourceOnlyPackageWithoutPayloadExpansion(t *testing.T) {
	path, digest := publishZIPLPK(t, map[string]zipTestEntry{
		"package.yml":                 {contents: []byte("package: cloud.lazycat.apps.publish-demo\nversion: 1.0.0\n")},
		"exports/config/default/data": {contents: bytes.Repeat([]byte{0}, 2<<20)},
	})
	provider := &countingTokenProvider{token: "ci-token"}
	server, requests := successfulPublishServer(t, digest)

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: provider, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || provider.calls != 1 || *requests != 3 {
		t.Fatalf("result=%#v provider calls=%d network requests=%d", result, provider.calls, *requests)
	}
}

type zipTestEntry struct {
	contents []byte
	mode     os.FileMode
}

func publishZIPLPK(t *testing.T, entries map[string]zipTestEntry) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "application.lpk")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, entry := range entries {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		output, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := output.Write(entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(writer.Close(), file.Close()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return path, fmt.Sprintf("%x", digest[:])
}

func successfulPublishServer(t *testing.T, digest string) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
