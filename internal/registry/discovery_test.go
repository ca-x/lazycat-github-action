package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/versioning"
	registryserver "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestCandidatesAutomaticallyScansLargeUpstream(testContext *testing.T) {
	for _, count := range []int{10001, 50001} {
		testContext.Run(strconv.Itoa(count), func(testContext *testing.T) {
			manifestRequests := 0
			pages := 0
			backend := registryserver.New()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if strings.HasSuffix(request.URL.Path, "/tags/list") {
					pages++
					first, _ := strconv.Atoi(request.URL.Query().Get("offset"))
					last := min(first+1000, count)
					var tags []string
					for index := first; index < last; index++ {
						tags = append(tags, fmt.Sprintf("0.1.%d", index))
					}
					if last < count {
						writer.Header().Set("Link", fmt.Sprintf("</v2/acme/large/tags/list?offset=%d>; rel=\"next\"", last))
					}
					json.NewEncoder(writer).Encode(map[string]any{"name": "acme/large", "tags": tags})
					return
				}
				if request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/manifests/") {
					manifestRequests++
				}
				backend.ServeHTTP(writer, request)
			}))
			defer server.Close()
			repository := internalTestRepository(testContext, server.URL, "acme/large")
			latest := fmt.Sprintf("0.1.%d", count-1)
			writeInternalTestImage(testContext, repository.Tag(latest), internalTestImageAt(testContext, "amd64", internalTestDay(1)), server)
			rule := versioning.Rule{Channel: versioning.ChannelStable, Sort: versioning.SortSemVer, VersionTemplate: "{version}"}
			candidates, err := New().Candidates(testContext.Context(), repository.Name(), TagFilter{SemVerRule: &rule})
			if err != nil {
				testContext.Fatal(err)
			}
			if len(candidates) != 1 || candidates[0].Tag != latest || pages != (count+999)/1000 || manifestRequests != 1 {
				testContext.Fatalf("candidates=%v pages=%d manifest requests=%d", candidates, pages, manifestRequests)
			}
		})
	}
}

func TestCandidatesFailBeforeInspectionOnPaginationErrors(testContext *testing.T) {
	for _, test := range []struct {
		name, next, want string
		pageFailure      bool
	}{
		{name: "self loop", next: "/v2/acme/broken/tags/list?n=1000", want: "pagination"},
		{name: "foreign JSON next", next: "https://other.invalid/v2/acme/broken/tags/list", want: "pagination"},
		{name: "wrong path", next: "/v2/acme/other/tags/list?offset=1", want: "pagination"},
		{name: "late HTTP failure", next: "/v2/acme/broken/tags/list?offset=1", pageFailure: true, want: "500"},
	} {
		testContext.Run(test.name, func(testContext *testing.T) {
			manifestRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/v2/" {
					return
				}
				if strings.HasSuffix(request.URL.Path, "/tags/list") {
					if test.pageFailure && request.URL.Query().Get("offset") != "" {
						writer.WriteHeader(http.StatusInternalServerError)
						return
					}
					json.NewEncoder(writer).Encode(map[string]any{"name": "acme/broken", "tags": []string{"1.2.3"}, "next": test.next})
					return
				}
				manifestRequests++
				writer.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()
			repository := internalTestRepository(testContext, server.URL, "acme/broken")
			_, err := New(remote.WithRetryPredicate(func(error) bool { return false })).Candidates(testContext.Context(), repository.Name())
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) || manifestRequests != 0 {
				testContext.Fatalf("err=%v manifest requests=%d", err, manifestRequests)
			}
		})
	}
}

func TestFilterTagsAutomaticMatchingHasNoDefaultCountCap(testContext *testing.T) {
	tags := make([]string, 10001)
	for index := range tags {
		tags[index] = fmt.Sprintf("1.0.%d", index)
	}
	eligible, err := filterTags("ghcr.io/acme/app", tags, TagFilter{})
	if err != nil || len(eligible) != len(tags) {
		testContext.Fatalf("eligible=%d err=%v", len(eligible), err)
	}
}

func TestCandidatesExplicitLimitsAndDuplicates(testContext *testing.T) {
	for _, test := range []struct {
		name   string
		filter TagFilter
		want   string
	}{
		{name: "raw limit counts duplicates", filter: TagFilter{MaxTags: 2}, want: "raw limit is 2"},
		{name: "matching limit", filter: TagFilter{MaxMatchingTags: 1}, want: "matching the configured filter"},
		{name: "matching names are unique", filter: TagFilter{MaxMatchingTags: 1, Exclude: regexp.MustCompile(`^2\.`)}},
	} {
		testContext.Run(test.name, func(testContext *testing.T) {
			backend := registryserver.New()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if strings.HasSuffix(request.URL.Path, "/tags/list") {
					fmt.Fprint(writer, `{"name":"acme/limits","tags":["1.0.0","1.0.0","2.0.0"]}`)
					return
				}
				backend.ServeHTTP(writer, request)
			}))
			defer server.Close()
			repository := internalTestRepository(testContext, server.URL, "acme/limits")
			writeInternalTestImage(testContext, repository.Tag("1.0.0"), internalTestImageAt(testContext, "amd64", internalTestDay(1)), server)
			candidates, err := New().Candidates(testContext.Context(), repository.Name(), test.filter)
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					testContext.Fatalf("err=%v", err)
				}
			} else if err != nil || len(candidates) != 1 {
				testContext.Fatalf("candidates=%v err=%v", candidates, err)
			}
		})
	}
}
