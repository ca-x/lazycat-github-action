package releasenotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/httpx"
	"github.com/ca-x/lazycat-github-action/internal/licenselookup"
)

// Repository accepts owner/repo or a GitHub repository homepage.
func Repository(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		return licenselookup.Repository(value)
	}
	if strings.Count(value, "/") != 1 {
		return ""
	}
	return licenselookup.Repository("https://github.com/" + value)
}

type Release struct {
	Tag        string
	Body       string
	URL        string
	AssetNames []string
}

type Client struct {
	HTTPClient *http.Client
	Token      string
}

// Lookup never falls back to latest: only the supplied exact tags are considered.
func (client Client) Lookup(ctx context.Context, repository string, tags []string) (Release, error) {
	repository = Repository(repository)
	if repository == "" {
		return Release{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	httpClient := httpx.NoRedirect(client.HTTPClient, 10*time.Second)
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repository+"/releases/tags/"+url.PathEscape(tag), nil)
		if err != nil {
			return Release{}, errors.New("cannot create GitHub release request")
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if client.Token != "" {
			request.Header.Set("Authorization", "Bearer "+client.Token)
		}
		response, err := httpClient.Do(request)
		if err != nil {
			return Release{}, errors.New("GitHub release request failed")
		}
		release, err := readRelease(response, tag)
		if err != nil {
			return Release{}, err
		}
		if strings.TrimSpace(release.Body) != "" {
			return release, nil
		}
	}
	return Release{}, nil
}

func readRelease(response *http.Response, tag string) (Release, error) {
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return Release{}, nil
	}
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub release lookup returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return Release{}, errors.New("GitHub release response exceeds limit or is unreadable")
	}
	var payload struct {
		Tag    string `json:"tag_name"`
		Body   string `json:"body"`
		URL    string `json:"html_url"`
		Draft  bool   `json:"draft"`
		Assets []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Release{}, errors.New("invalid GitHub release response")
	}
	if payload.Draft || payload.Tag != tag || len(payload.Body) > MaxChangelogBytes {
		return Release{}, nil
	}
	release := Release{Tag: payload.Tag, Body: payload.Body, URL: payload.URL}
	for _, asset := range payload.Assets {
		release.AssetNames = append(release.AssetNames, asset.Name)
	}
	return release, nil
}
