package licenselookup

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const mitLicense = `MIT License

Copyright (c) 2026 Example

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.`

type roundTrip func(*http.Request) (*http.Response, error)

func (fn roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestLookup(t *testing.T) {
	for _, test := range []struct {
		name, spdx, content, encoding, want string
		status                              int
		wantErr                             bool
	}{
		{name: "API SPDX takes precedence", spdx: "Apache-2.0", content: mitLicense, encoding: "base64", want: "Apache-2.0", status: 200},
		{name: "API SPDX needs no text", spdx: "MIT", want: "MIT", status: 200},
		{name: "NOASSERTION falls back to text", spdx: "NOASSERTION", content: mitLicense, encoding: "base64", want: "MIT", status: 200},
		{name: "missing SPDX falls back to text", content: mitLicense, encoding: "base64", want: "MIT", status: 200},
		{name: "unknown text", content: "All rights reserved. Contact author for terms.", encoding: "base64", status: 200},
		{name: "URL alone is not a license", content: "https://opensource.org/licenses/MIT", encoding: "base64", status: 200},
		{name: "not found", status: 404},
		{name: "rate limited", status: 403, wantErr: true},
		{name: "server failure", status: 503, wantErr: true},
		{name: "unsupported encoding", status: 200, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{"license": map[string]string{"spdx_id": test.spdx}, "content": base64.StdEncoding.EncodeToString([]byte(test.content)), "encoding": test.encoding})
			client := Client{Token: "test-token", HTTPClient: &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://api.github.com/repos/google/licensecheck/license" || req.Header.Get("Authorization") != "Bearer test-token" {
					t.Fatalf("request=%v", req)
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(string(payload))), Header: make(http.Header)}, nil
			})}}
			got, err := client.Lookup(t.Context(), "https://github.com/google/licensecheck")
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("got=%q err=%v want=%q", got, err, test.want)
			}
		})
	}
}

func TestRepository(t *testing.T) {
	for input, want := range map[string]string{
		"http://www.github.com/google/licensecheck":        "google/licensecheck",
		"https://github.com/google/licensecheck":           "google/licensecheck",
		"https://github.com/google/licensecheck.git/":      "google/licensecheck",
		"https://github.com/google/licensecheck/tree/main": "google/licensecheck",
		"https://example.com/google/licensecheck":          "",
		"https://github.com.evil.com/google/licensecheck":  "",
		"https://github.com/google":                        "",
		"https://secret@github.com/google/licensecheck":    "",
		"https://github.com/google/..":                     "",
		"https://github.com/google/%2Flicensecheck":        "",
	} {
		if got := Repository(input); got != want {
			t.Errorf("Repository(%q)=%q want %q", input, got, want)
		}
	}
}
