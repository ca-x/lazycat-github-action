package releasenotes_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/releasenotes"
)

func TestRender(t *testing.T) {
	data := releasenotes.Data{Version: "1.2.3", Tag: "v1.2.3", PackageID: "cloud.example.app", Name: "Example", ReleaseNotes: "## Changes\n\n- Fix startup\n- Improve UI"}
	for _, test := range []struct{ name, source, notes, want string }{
		{"preserve Markdown whitespace", "", "    first()\n    second()\n", "    first()\n    second()\n"},
		{"default release body", "", data.ReleaseNotes, data.ReleaseNotes},
		{"default fallback", "", "", "Release 1.2.3"},
		{"custom wrapper", "{{.Name}} {{.Tag}} ({{.PackageID}})\n\n{{.ReleaseNotes}}", data.ReleaseNotes, "Example v1.2.3 (cloud.example.app)\n\n" + data.ReleaseNotes},
		{"custom fallback", "更新至 {{.Version}}", "", "更新至 1.2.3"},
		{"empty rendered fallback", "{{.ReleaseNotes}}", "", "Release 1.2.3"},
		{"body is never evaluated", "{{.ReleaseNotes}}", "{{.Unknown}} $HOME `command`", "{{.Unknown}} $HOME `command`"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := data
			value.ReleaseNotes = test.notes
			got, err := releasenotes.Render(test.source, value)
			if err != nil || got != test.want {
				t.Fatalf("Render()=%q, %v; want %q", got, err, test.want)
			}
		})
	}
	for _, source := range []string{"{{", "{{.Unknown}}", "{{if .ReleaseNotes}}{{.Unknown}}{{end}}", "{{if .Name}}{{.Unknown}}{{end}}", "{{$.Unknown}}"} {
		if err := releasenotes.ValidateTemplate(source); err == nil {
			t.Errorf("accepted invalid template %q", source)
		}
	}
	if _, err := releasenotes.Render("{{.ReleaseNotes}}{{.ReleaseNotes}}", releasenotes.Data{ReleaseNotes: strings.Repeat("x", 1<<20)}); err == nil {
		t.Fatal("accepted excessive rendered notes")
	}
}

func TestRepository(t *testing.T) {
	for input, want := range map[string]string{
		"acme/app":                              "acme/app",
		" https://github.com/acme/app.git ":     "acme/app",
		"https://github.com/acme/app/tree/main": "acme/app",
		"https://example.com/acme/app":          "",
		"https://secret@github.com/acme/app":    "",
		"../app":                                "",
		"acme/app/extra":                        "",
	} {
		if got := releasenotes.Repository(input); got != want {
			t.Errorf("Repository(%q)=%q, want %q", input, got, want)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLookupUsesExactTagsAndPreservesMarkdown(t *testing.T) {
	var paths []string
	client := releasenotes.Client{Token: "test-token", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected request: %s", r.URL)
		}
		if strings.HasSuffix(r.URL.Path, "/v1.2.3") {
			return response(http.StatusNotFound, `{}`), nil
		}
		return response(http.StatusOK, `{"tag_name":"1.2.3","body":"## Changes\n\n- Fixed","html_url":"https://github.com/acme/app/releases/tag/1.2.3"}`), nil
	})}}
	got, err := client.Lookup(t.Context(), "acme/app", []string{"v1.2.3", "v1.2.3", "1.2.3"})
	if err != nil || got.Tag != "1.2.3" || got.Body != "## Changes\n\n- Fixed" || got.URL != "https://github.com/acme/app/releases/tag/1.2.3" {
		t.Fatalf("Lookup()=%#v, %v", got, err)
	}
	if !reflect.DeepEqual(paths, []string{"/repos/acme/app/releases/tags/v1.2.3", "/repos/acme/app/releases/tags/1.2.3"}) {
		t.Fatalf("paths=%v", paths)
	}
}

func TestLookupRejectsUnavailableOrUnrelatedNotes(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"missing", `{}`, 404, false},
		{"empty", `{"tag_name":"v1.2.3","body":"  \n"}`, 200, false},
		{"null", `{"tag_name":"v1.2.3","body":null}`, 200, false},
		{"draft", `{"tag_name":"v1.2.3","body":"draft","draft":true}`, 200, false},
		{"wrong version", `{"tag_name":"v9.0.0","body":"wrong"}`, 200, false},
		{"rate limit", `secret-response`, 403, true},
		{"server error", `secret-response`, 500, true},
		{"invalid JSON", `secret-response`, 200, true},
		{"oversized", strings.Repeat("x", (1<<20)+1), 200, true},
		{"body exceeds safe transport size", `{"tag_name":"v1.2.3","body":"` + strings.Repeat("中", 50000) + `"}`, 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := releasenotes.Client{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(test.status, test.body), nil })}}
			got, err := client.Lookup(t.Context(), "acme/app", []string{"v1.2.3"})
			if got.Body != "" || (err != nil) != test.wantErr {
				t.Fatalf("Lookup()=%#v, %v", got, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-response") {
				t.Fatal("error leaks response")
			}
		})
	}
}

func TestLookupDoesNotFollowRedirectsOrLeakTransportErrors(t *testing.T) {
	for _, redirected := range []bool{false, true} {
		t.Run(fmt.Sprint(redirected), func(t *testing.T) {
			calls := 0
			client := releasenotes.Client{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if !redirected {
					return nil, errors.New("secret-token")
				}
				r := response(http.StatusFound, "")
				r.Header.Set("Location", "https://other.example/steal")
				return r, nil
			})}}
			_, err := client.Lookup(t.Context(), "acme/app", []string{"v1.2.3"})
			if err == nil || strings.Contains(err.Error(), "secret-token") || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestLookupSkipsInvalidRepositories(t *testing.T) {
	client := releasenotes.Client{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid repository requested"); return nil, nil })}}
	for _, repo := range []string{"", "../app", "https://elsewhere.example/app"} {
		if _, err := client.Lookup(t.Context(), repo, []string{"v1.2.3"}); err != nil {
			t.Fatal(err)
		}
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestTemplateValidationDoesNotEvaluateSyntheticValues(t *testing.T) {
	const source = "{{slice .PackageID 0 3}}"
	if err := releasenotes.ValidateTemplate(source); err != nil {
		t.Fatal(err)
	}
	got, err := releasenotes.Render(source, releasenotes.Data{PackageID: "cloud.example.app"})
	if err != nil || got != "clo" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestRenderRejectsNotesTooLargeForActionInputs(t *testing.T) {
	if _, err := releasenotes.Render("{{.ReleaseNotes}}", releasenotes.Data{ReleaseNotes: strings.Repeat("中", 50000)}); err == nil {
		t.Fatal("accepted notes that cannot be forwarded as one Action environment input")
	}
}
