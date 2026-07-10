package private_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/store/private"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const (
	packageID   = "cloud.lazycat.example.app"
	version     = "1.2.3"
	downloadURL = "https://github.com/acme/example/releases/download/v1.2.3/app.lpk"
	digest      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestClientCreatesApplicationWithExternalVersion(t *testing.T) {
	var created bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer lcst_test" {
			t.Errorf("authorization=%q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/apps":
			if request.URL.Query().Get("q") != packageID {
				t.Errorf("q=%q", request.URL.Query().Get("q"))
			}
			_, _ = response.Write([]byte(`{"apps":[]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/apps":
			created = true
			body, _ := io.ReadAll(request.Body)
			text := string(body)
			for _, expected := range []string{`"packageId":"` + packageID + `"`, `"version":"1.2.3"`, `"sourceType":"GITHUB"`, `"downloadUrl":"` + downloadURL + `"`, `"sha256":"` + digest + `"`} {
				if !strings.Contains(text, expected) {
					t.Errorf("request body=%s missing %s", text, expected)
				}
			}
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{"app":{"id":17,"packageId":"cloud.lazycat.example.app","latestVersion":{"id":23,"appId":17,"version":"1.2.3","downloadUrl":"https://github.com/acme/example/releases/download/v1.2.3/app.lpk","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Publish(context.Background(), private.Request{
		PackageID: packageID, Name: "Example App", Summary: "Published from CI", Version: version,
		Changelog: "Release notes", DownloadURL: downloadURL, SHA256: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || !result.Published || !result.Created || result.Existing || result.AppID != "17" || result.VersionID != "23" || result.PackageID != packageID || result.Version != version || result.DownloadURL != downloadURL || result.SHA256 != digest {
		t.Fatalf("created=%v result=%#v", created, result)
	}
}

func TestClientReusesExistingVersionByAppID(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			posts++
		}
		_, _ = response.Write([]byte(`{"app":{"id":42,"packageId":"cloud.lazycat.example.app","versions":[{"id":55,"appId":42,"version":"1.2.3","downloadUrl":"https://github.com/acme/example/releases/download/v1.2.3/app.lpk","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}`))
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Publish(context.Background(), private.Request{AppID: "42", PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
	if err != nil {
		t.Fatal(err)
	}
	if posts != 0 || !result.Published || !result.Existing || result.Created || result.VersionID != "55" {
		t.Fatalf("posts=%d result=%#v", posts, result)
	}
}

func TestClientCreatesExternalVersionForExistingApp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			_, _ = response.Write([]byte(`{"app":{"id":"42","packageId":"cloud.lazycat.example.app","versions":[]}}`))
		case http.MethodPost:
			if request.URL.Path != "/api/v1/apps/42/versions" || request.Header.Get("Content-Type") != "application/json" {
				t.Errorf("path=%q content-type=%q", request.URL.Path, request.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(request.Body)
			if strings.Contains(string(body), "file") || !strings.Contains(string(body), `"downloadUrl":"`+downloadURL+`"`) {
				t.Errorf("body=%s", body)
			}
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{"version":{"id":56,"appId":42,"version":"1.2.3","downloadUrl":"https://github.com/acme/example/releases/download/v1.2.3/app.lpk","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`))
		}
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Publish(context.Background(), private.Request{AppID: "42", PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
	if err != nil || result.VersionID != "56" || result.Created || result.Existing {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestClientValidatesReleaseURLAndSHA(t *testing.T) {
	tests := []struct {
		name string
		url  string
		sha  string
	}{
		{name: "http", url: "http://github.com/acme/example/releases/download/v1/app.lpk", sha: digest},
		{name: "host", url: "https://example.com/acme/example/releases/download/v1/app.lpk", sha: digest},
		{name: "path", url: "https://github.com/acme/example/archive/v1.zip", sha: digest},
		{name: "userinfo", url: "https://user@github.com/acme/example/releases/download/v1/app.lpk", sha: digest},
		{name: "fragment", url: downloadURL + "#secret", sha: digest},
		{name: "sha", url: downloadURL, sha: "bad"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := private.New(private.Options{BaseURL: "https://store.example.com", Token: "lcst_test"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Publish(context.Background(), private.Request{PackageID: packageID, Name: "Example", Version: version, DownloadURL: test.url, SHA256: test.sha})
			if !errors.Is(err, lpkgo.ErrInvalidArgument) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestClientDoesNotExposeRemoteAuthenticationBody(t *testing.T) {
	const secretBody = "token lcst_must_not_leak"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(secretBody))
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Publish(context.Background(), private.Request{PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
	if !errors.Is(err, lpkgo.ErrUnauthenticated) || strings.Contains(err.Error(), secretBody) {
		t.Fatalf("err=%v", err)
	}
}

func TestClientRejectsOversizedAndMalformedResponses(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "oversized", body: strings.Repeat("x", (1<<20)+1)},
		{name: "malformed", body: "not-json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Publish(context.Background(), private.Request{PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
			if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
