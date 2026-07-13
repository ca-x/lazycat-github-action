package official_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
