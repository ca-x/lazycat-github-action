package official_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/store/official"
	"github.com/lib-x/lzc-toolkit-go/auth"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

func TestPublisherUsesToolkitProtocolAndReturnsVerifiedResult(t *testing.T) {
	path, digest := publishLPK(t)
	var created, uploaded, reviewed bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-User-Token") != "ci-token" {
			t.Errorf("missing X-User-Token")
		}
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":false}`))
		case "/api/v3/developer/app/create":
			created = true
			var input map[string]any
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input["package"] != "cloud.lazycat.apps.publish-demo" || input["language"] != "zh" || input["name"] != "Configured Name" {
				t.Errorf("create input=%#v", input)
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploaded = true
			if err := request.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
			}
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			reviewed = true
			var body struct {
				Version struct {
					Changelogs map[string]string `json:"changelogs"`
				} `json:"version"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Version.Changelogs["zh"] != "Release notes" || body.Version.Changelogs["en"] != "Release notes" {
				t.Errorf("changelogs=%#v", body.Version.Changelogs)
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, FileName: "application.lpk",
		PackageID: "cloud.lazycat.apps.publish-demo", Version: "1.0.0", SHA256: digest,
		Changelog: "Release notes", Locales: []string{"zh", "en"}, CreateIfMissing: true,
		Application: config.OfficialApplication{Language: "zh", Name: "Configured Name"}, DefaultName: "Package Name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || !uploaded || !reviewed || !result.Published || !result.Created || result.PackageID != "cloud.lazycat.apps.publish-demo" || result.Version != "1.0.0" || result.SHA256 != digest || result.UploadURL != "/demo.lpk" {
		t.Fatalf("created=%v uploaded=%v reviewed=%v result=%#v", created, uploaded, reviewed, result)
	}
}

func TestPublisherRejectsUntrustedUploadMetadata(t *testing.T) {
	path, digest := publishLPK(t)
	reviewed := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			_, _ = response.Write([]byte(`{"package":"cloud.lazycat.apps.publish-demo","version":"9.9.9","url":"/demo.lpk","sha256":"bad"}`))
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			reviewed = true
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err == nil || !reviewed {
		t.Fatalf("err=%v reviewed=%v", err, reviewed)
	}
}

func TestPublisherDoesNotForwardTokenAcrossRedirect(t *testing.T) {
	path, digest := publishLPK(t)
	reached := false
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		reached = true
		forwarded = request.Header.Get("X-User-Token") != "" || request.Header.Get("Cookie") != ""
		_, _ = response.Write([]byte(`{"exist":true}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	_, err := (official.Publisher{BaseURL: origin.URL, HTTPClient: origin.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err == nil || reached || forwarded {
		t.Fatalf("err=%v reached=%v forwarded=%v", err, reached, forwarded)
	}
}

func publishLPK(t *testing.T) (string, string) {
	t.Helper()
	root := fstest.MapFS{
		"package.yml":  {Data: []byte("package: cloud.lazycat.apps.publish-demo\nversion: 1.0.0\nname: Publish Demo\nmin_os_version: 1.3.0\nlocales:\n  en:\n    name: Publish Demo\n"), Mode: 0o644},
		"manifest.yml": {Data: []byte("application:\n  subdomain: publish-demo\n  image: registry.lazycat.cloud/demo/app:1.0.0\n"), Mode: 0o644},
		"icon.png":     {Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, Mode: 0o644},
	}
	path := filepath.Join(t.TempDir(), "application.lpk")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lpk.Write(context.Background(), file, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: root, Strict: true}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return path, fmt.Sprintf("%x", digest[:])
}
