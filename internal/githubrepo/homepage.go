// Package githubrepo updates metadata of the current GitHub Actions repository.
package githubrepo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/httpx"
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]+$`)

type Client struct {
	ServerURL  string
	Repository string
	Token      string
	HTTPClient *http.Client
}

// EnsureHomepage fills only an empty Website field. It never visits the supplied URL.
func (client Client) EnsureHomepage(ctx context.Context, homepage string) (bool, error) {
	if strings.TrimRight(strings.TrimSpace(client.ServerURL), "/") != "https://github.com" || !repositoryPattern.MatchString(client.Repository) {
		return false, nil
	}
	_, repoName, _ := strings.Cut(client.Repository, "/")
	if repoName == "." || repoName == ".." {
		return false, nil
	}
	homepage = strings.TrimSpace(homepage)
	parsed, err := url.Parse(homepage)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return false, nil
	}
	if client.Token == "" {
		return false, errors.New("GitHub repository metadata token is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	endpoint := "https://api.github.com/repos/" + client.Repository
	httpClient := httpx.NoRedirect(client.HTTPClient, 10*time.Second)
	request := func(method string, body io.Reader) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return nil, errors.New("cannot create GitHub repository metadata request")
		}
		req.Header.Set("Authorization", "Bearer "+client.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := httpClient.Do(req)
		if err != nil {
			return nil, errors.New("GitHub repository metadata request failed")
		}
		return response, nil
	}
	response, err := request(http.MethodGet, nil)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GitHub repository lookup returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return false, errors.New("GitHub repository response is unreadable or too large")
	}
	var current struct {
		Homepage json.RawMessage `json:"homepage"`
	}
	if err := json.Unmarshal(data, &current); err != nil || len(current.Homepage) == 0 {
		return false, errors.New("GitHub repository response is missing its homepage field")
	}
	var existing *string
	if err := json.Unmarshal(current.Homepage, &existing); err != nil {
		return false, errors.New("GitHub repository homepage is invalid")
	}
	if existing != nil && strings.TrimSpace(*existing) != "" {
		return false, nil
	}
	payload, _ := json.Marshal(struct {
		Homepage string `json:"homepage"`
	}{homepage})
	updated, err := request(http.MethodPatch, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	defer updated.Body.Close()
	if updated.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GitHub repository update returned HTTP %d", updated.StatusCode)
	}
	return true, nil
}
