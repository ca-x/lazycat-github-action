package githubrepo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (fn transport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestEnsureHomepage(t *testing.T) {
	for _, test := range []struct {
		name, body                      string
		getStatus, patchStatus          int
		wantPatch, wantUpdated, wantErr bool
	}{
		{name: "null website", body: `{"homepage":null}`, getStatus: 200, patchStatus: 200, wantPatch: true, wantUpdated: true},
		{name: "empty website", body: `{"homepage":""}`, getStatus: 200, patchStatus: 200, wantPatch: true, wantUpdated: true},
		{name: "existing website", body: `{"homepage":"https://existing.example"}`, getStatus: 200},
		{name: "missing field is not empty", body: `{}`, getStatus: 200, wantErr: true},
		{name: "invalid JSON", body: `{`, getStatus: 200, wantErr: true},
		{name: "invalid field", body: `{"homepage":42}`, getStatus: 200, wantErr: true},
		{name: "lookup forbidden", getStatus: 403, wantErr: true},
		{name: "write forbidden", body: `{"homepage":""}`, getStatus: 200, patchStatus: 403, wantPatch: true, wantErr: true},
		{name: "redirect is not followed", getStatus: 302, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := Client{ServerURL: "https://github.com", Repository: "owner/app-package", Token: "secret", HTTPClient: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != "https://api.github.com/repos/owner/app-package" || req.Header.Get("Authorization") != "Bearer secret" {
					t.Fatalf("request=%v", req.URL)
				}
				status, body := test.getStatus, test.body
				if req.Method == http.MethodPatch {
					if !test.wantPatch {
						t.Fatal("existing or uncertain homepage was overwritten")
					}
					var payload map[string]string
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if len(payload) != 1 || payload["homepage"] != "https://upstream.example/app" {
						t.Fatalf("payload=%v", payload)
					}
					status, body = test.patchStatus, `{}`
				} else if req.Method != http.MethodGet {
					t.Fatalf("method=%s", req.Method)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}
			updated, err := client.EnsureHomepage(t.Context(), "https://upstream.example/app")
			wantCalls := 1
			if test.wantPatch {
				wantCalls = 2
			}
			if updated != test.wantUpdated || (err != nil) != test.wantErr || calls != wantCalls {
				t.Fatalf("updated=%v err=%v calls=%d", updated, err, calls)
			}
		})
	}
}

func TestUnsupportedHostsAndInvalidURLsNeverMakeRequests(t *testing.T) {
	for _, test := range []struct{ server, repo, homepage string }{
		{server: "https://gitea.example", repo: "owner/app", homepage: "https://upstream.example"},
		{server: "", repo: "owner/app", homepage: "https://upstream.example"},
		{server: "https://github.com", repo: "../app", homepage: "https://upstream.example"},
		{server: "https://github.com", repo: "owner/app", homepage: ""},
		{server: "https://github.com", repo: "owner/app", homepage: "javascript:alert(1)"},
		{server: "https://github.com", repo: "owner/app", homepage: "https://password@upstream.example"},
	} {
		client := Client{ServerURL: test.server, Repository: test.repo, Token: "secret", HTTPClient: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected remote request"); return nil, nil })}}
		if updated, err := client.EnsureHomepage(t.Context(), test.homepage); updated || err != nil {
			t.Fatalf("test=%+v updated=%v err=%v", test, updated, err)
		}
	}
}
