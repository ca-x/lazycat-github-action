package registry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	tagDiscoveryTimeout = 10 * time.Minute
	tagDiscoveryBudget  = 32 << 20
	tagPageBodyLimit    = 4 << 20
	tagPageSize         = 1000
)

type discoveryBudget struct {
	remaining int
}

func (budget *discoveryBudget) retain(value string) error {
	cost := len(value) + 64
	if cost > budget.remaining {
		return publicError{detail: "image tag discovery exceeded its 32 MiB retained-state budget; narrow tag_regex or exclude_regex"}
	}
	budget.remaining -= cost
	return nil
}

type pageTracker struct {
	origin  *url.URL
	visited map[string]struct{}
	budget  *discoveryBudget
}

func newPageTracker(initial string, budget *discoveryBudget) (*pageTracker, error) {
	origin, err := url.Parse(initial)
	if err != nil {
		return nil, errors.New("invalid tag pagination origin")
	}
	origin.RawQuery = origin.Query().Encode()
	initial = origin.String()
	if err := budget.retain(initial); err != nil {
		return nil, err
	}
	return &pageTracker{origin: origin, visited: map[string]struct{}{initial: {}}, budget: budget}, nil
}

func (tracker *pageTracker) next(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parsed, err := resolvePageURL(tracker.origin, raw)
	if err != nil {
		return "", err
	}
	next := parsed.String()
	if _, exists := tracker.visited[next]; exists {
		return "", publicError{detail: "image tag pagination repeated a previously visited page"}
	}
	if err := tracker.budget.retain(next); err != nil {
		return "", err
	}
	tracker.visited[next] = struct{}{}
	return next, nil
}

func resolvePageURL(origin *url.URL, raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, publicError{detail: "invalid image tag pagination URL"}
	}
	parsed = origin.ResolveReference(parsed)
	if parsed.Scheme != origin.Scheme || parsed.Host != origin.Host || parsed.EscapedPath() != origin.EscapedPath() || parsed.User != nil || parsed.Fragment != "" {
		return nil, publicError{detail: "image tag pagination URL must keep the same registry and repository path"}
	}
	parsed.RawQuery = parsed.Query().Encode()
	return parsed, nil
}

type boundedTagTransport struct {
	base http.RoundTripper
}

func (transport boundedTagTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil || !strings.HasSuffix(request.URL.Path, "/tags/list") {
		return response, err
	}
	if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
		if _, err := resolvePageURL(request.URL, response.Header.Get("Location")); err != nil {
			response.Body.Close()
			return nil, publicError{detail: "image tag pagination redirect must keep the same registry and repository path"}
		}
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, tagPageBodyLimit+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return nil, fmt.Errorf("read image tag page: %w", errors.Join(readErr, closeErr))
	}
	if len(body) > tagPageBodyLimit {
		return nil, publicError{detail: "image tag page exceeds the 4 MiB response limit"}
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

func (client *Client) listMatchingTags(ctx context.Context, repository name.Repository, target platform.Target, filter TagFilter, budget *discoveryBudget) ([]string, error) {
	initial := url.URL{Scheme: repository.Scheme(), Host: repository.RegistryStr(), Path: "/v2/" + repository.RepositoryStr() + "/tags/list"}
	initial.RawQuery = fmt.Sprintf("n=%d", tagPageSize)
	tracker, err := newPageTracker(initial.String(), budget)
	if err != nil {
		return nil, err
	}
	options := append(client.remoteOptions(ctx, target), remote.WithPageSize(tagPageSize))
	puller, err := remote.NewPuller(options...)
	if err != nil {
		return nil, err
	}
	lister, err := puller.Lister(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("list image tags for %q: %w", repository.Name(), err)
	}
	seen := make(map[string]struct{})
	var eligible []string
	rawCount := 0
	for lister.HasNext() {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		page, err := lister.Next(ctx)
		if err != nil {
			return nil, fmt.Errorf("list image tags for %q: %w", repository.Name(), err)
		}
		page.Next, err = tracker.next(page.Next)
		if err != nil {
			return nil, err
		}
		for _, tag := range page.Tags {
			rawCount++
			if filter.MaxTags > 0 && rawCount > filter.MaxTags {
				return nil, publicError{detail: fmt.Sprintf("image repository returned more than %d tags; raw limit is %d", filter.MaxTags, filter.MaxTags)}
			}
			if filter.Include != nil && !filter.Include.MatchString(tag) || filter.Exclude != nil && filter.Exclude.MatchString(tag) {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			if filter.MaxMatchingTags > 0 && len(eligible) >= filter.MaxMatchingTags {
				return nil, publicError{detail: fmt.Sprintf("image repository has more than %d tags matching the configured filter; limit is %d", filter.MaxMatchingTags, filter.MaxMatchingTags)}
			}
			if err := budget.retain(tag); err != nil {
				return nil, err
			}
			seen[tag] = struct{}{}
			eligible = append(eligible, tag)
		}
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	slices.Sort(eligible)
	return eligible, nil
}
