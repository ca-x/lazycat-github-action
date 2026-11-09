package registry

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/versioning"
	registryserver "github.com/google/go-containerregistry/pkg/registry"
)

func TestCandidatesReadsShortAndEmptyPagesBeforePlatformFallback(testContext *testing.T) {
	backend := registryserver.New()
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			page := request.URL.Query().Get("offset")
			pages = append(pages, page)
			switch page {
			case "":
				fmt.Fprint(writer, `{"tags":["1.0.0","1.0.0","ignored"],"next":"?offset=1"}`)
			case "1":
				fmt.Fprint(writer, `{"tags":[],"next":"?offset=2"}`)
			case "2":
				fmt.Fprint(writer, `{"tags":["10.0.0","2.0.0"]}`)
			}
			return
		}
		backend.ServeHTTP(writer, request)
	}))
	defer server.Close()
	repository := internalTestRepository(testContext, server.URL, "acme/paged-platforms")
	writeInternalTestImage(testContext, repository.Tag("10.0.0"), internalTestImageAt(testContext, "arm64", internalTestDay(1)), server)
	writeInternalTestImage(testContext, repository.Tag("2.0.0"), internalTestImageAt(testContext, "amd64", internalTestDay(1)), server)
	rule := versioning.Rule{Channel: versioning.ChannelStable, Sort: versioning.SortSemVer, VersionTemplate: "{version}"}
	candidates, err := New().Candidates(testContext.Context(), repository.Name(), TagFilter{
		Exclude: regexp.MustCompile(`^ignored$`), SemVerRule: &rule, MaxMatchingTags: 3,
	})
	if err != nil || len(candidates) != 1 || candidates[0].Tag != "2.0.0" || len(pages) != 3 {
		testContext.Fatalf("candidates=%v pages=%v err=%v", candidates, pages, err)
	}
}

func TestDefaultTagTransportBoundsEntireBody(testContext *testing.T) {
	for _, chunked := range []bool{false, true} {
		testContext.Run(fmt.Sprintf("chunked=%t", chunked), func(testContext *testing.T) {
			manifestRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/v2/" {
					return
				}
				if strings.HasSuffix(request.URL.Path, "/tags/list") {
					body := `{"tags":["1.2.3"]}` + strings.Repeat(" ", tagPageBodyLimit)
					if chunked {
						writer.(http.Flusher).Flush()
					} else {
						writer.Header().Set("Content-Length", fmt.Sprint(len(body)))
					}
					fmt.Fprint(writer, body)
					return
				}
				manifestRequests++
				writer.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()
			repository := internalTestRepository(testContext, server.URL, "acme/body-limit")
			_, err := New().Candidates(testContext.Context(), repository.Name())
			if err == nil || !strings.Contains(err.Error(), "4 MiB") || manifestRequests != 0 {
				testContext.Fatalf("err=%v manifest requests=%d", err, manifestRequests)
			}
		})
	}
}

func TestTagDiscoveryBudgetIncludesNamesAndCursors(testContext *testing.T) {
	initial := "https://ghcr.io/v2/acme/app/tags/list?n=1000"
	budget := &discoveryBudget{remaining: len(initial) + 64 + len("1.2.3") + 64}
	tracker, err := newPageTracker(initial, budget)
	if err != nil {
		testContext.Fatal(err)
	}
	if err := budget.retain("1.2.3"); err != nil || budget.remaining != 0 {
		testContext.Fatalf("remaining=%d err=%v", budget.remaining, err)
	}
	if _, err := tracker.next("?n=1000&last=1.2.3"); err == nil || !strings.Contains(err.Error(), "budget") {
		testContext.Fatalf("err=%v", err)
	}
}

func TestTagPaginationRejectsRepeatedAndUnsafeCursors(testContext *testing.T) {
	for _, next := range []string{
		"?n=1000", "https://other.invalid/v2/acme/app/tags/list?n=1000",
		"http://ghcr.io/v2/acme/app/tags/list?n=1000", "/v2/acme/other/tags/list?last=1",
		"https://user@ghcr.io/v2/acme/app/tags/list?last=1", "?last=1#fragment",
	} {
		tracker, err := newPageTracker("https://ghcr.io/v2/acme/app/tags/list?n=1000", &discoveryBudget{remaining: 4096})
		if err != nil {
			testContext.Fatal(err)
		}
		if _, err := tracker.next(next); err == nil {
			testContext.Fatalf("unsafe or repeated cursor accepted: %s", next)
		}
	}
}

func TestCandidatesRejectsCancellationWithoutInspectingPartialResults(testContext *testing.T) {
	ctx, cancel := context.WithCancel(testContext.Context())
	defer cancel()
	manifestRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/" {
			return
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			cancel()
			fmt.Fprint(writer, `{"tags":["1.2.3"]}`)
			return
		}
		manifestRequests++
	}))
	defer server.Close()
	repository := internalTestRepository(testContext, server.URL, "acme/cancelled")
	_, err := New().Candidates(ctx, repository.Name())
	if !errors.Is(err, context.Canceled) || manifestRequests != 0 {
		testContext.Fatalf("err=%v manifest requests=%d", err, manifestRequests)
	}
}

func TestDefaultTagTransportBoundsDecompressedBody(testContext *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/" {
			return
		}
		writer.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(writer)
		fmt.Fprint(compressed, `{"tags":["1.2.3"]}`+strings.Repeat(" ", tagPageBodyLimit))
		compressed.Close()
	}))
	defer server.Close()
	repository := internalTestRepository(testContext, server.URL, "acme/compressed")
	_, err := New().Candidates(testContext.Context(), repository.Name())
	if err == nil || !strings.Contains(err.Error(), "4 MiB") {
		testContext.Fatalf("err=%v", err)
	}
}

func TestTagDiscoveryHonorsShorterCallerDeadline(testContext *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/" {
			return
		}
		<-request.Context().Done()
	}))
	defer server.Close()
	repository := internalTestRepository(testContext, server.URL, "acme/deadline")
	ctx, cancel := context.WithTimeout(testContext.Context(), 25*time.Millisecond)
	defer cancel()
	_, err := New().Candidates(ctx, repository.Name())
	if !errors.Is(err, context.DeadlineExceeded) {
		testContext.Fatalf("err=%v", err)
	}
}

func TestDefaultTagTransportRejectsRedirectsOutsideTagPath(testContext *testing.T) {
	redirectedRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/" {
			return
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			http.Redirect(writer, request, "/tag-page", http.StatusFound)
			return
		}
		redirectedRequests++
		fmt.Fprint(writer, `{"tags":[]}`+strings.Repeat(" ", tagPageBodyLimit))
	}))
	defer server.Close()
	repository := internalTestRepository(testContext, server.URL, "acme/redirected")
	_, err := New().Candidates(testContext.Context(), repository.Name())
	if err == nil || !strings.Contains(err.Error(), "redirect") || redirectedRequests != 0 {
		testContext.Fatalf("err=%v redirected requests=%d", err, redirectedRequests)
	}
}

func TestDefaultTagTransportBoundsSamePathRedirects(testContext *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/" {
			return
		}
		if request.URL.Query().Get("redirected") == "" {
			http.Redirect(writer, request, request.URL.Path+"?redirected=true", http.StatusFound)
			return
		}
		fmt.Fprint(writer, `{"tags":[]}`+strings.Repeat(" ", tagPageBodyLimit))
	}))
	defer server.Close()
	repository := internalTestRepository(testContext, server.URL, "acme/redirected")
	_, err := New().Candidates(testContext.Context(), repository.Name())
	if err == nil || !strings.Contains(err.Error(), "4 MiB") {
		testContext.Fatalf("err=%v", err)
	}
}
