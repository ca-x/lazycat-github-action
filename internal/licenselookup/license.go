package licenselookup

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/httpx"
	"github.com/google/licensecheck"
)

var spdxID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)

var repositoryPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Repository accepts GitHub repository homepages, including links within a repository.
func Repository(homepage string) string {
	u, err := url.Parse(strings.TrimSpace(homepage))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || (!strings.EqualFold(u.Host, "github.com") && !strings.EqualFold(u.Host, "www.github.com")) || u.User != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
	if !repositoryPart.MatchString(owner) || !repositoryPart.MatchString(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return ""
	}
	return owner + "/" + repo
}

type Client struct {
	HTTPClient *http.Client
	Token      string
}

func (client Client) Lookup(ctx context.Context, homepage string) (string, error) {
	repo := Repository(homepage)
	if repo == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/license", nil)
	if err != nil {
		return "", fmt.Errorf("create GitHub license request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if client.Token != "" {
		request.Header.Set("Authorization", "Bearer "+client.Token)
	}
	response, err := httpx.NoRedirect(client.HTTPClient, 10*time.Second).Do(request)
	if err != nil {
		return "", fmt.Errorf("GitHub license request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub license lookup returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		License struct {
			SPDXID string `json:"spdx_id"`
		} `json:"license"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return "", fmt.Errorf("GitHub license response exceeds limit or is unreadable")
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("invalid GitHub license response")
	}
	apiLicense := strings.TrimSpace(payload.License.SPDXID)
	if apiLicense != "NOASSERTION" && apiLicense != "NONE" && spdxID.MatchString(apiLicense) {
		return apiLicense, nil
	}
	if payload.Encoding != "base64" {
		return "", fmt.Errorf("unsupported GitHub license encoding")
	}
	text, err := base64.StdEncoding.DecodeString(payload.Content)
	if err != nil {
		return "", fmt.Errorf("invalid GitHub license content")
	}
	coverage := licensecheck.Scan(text)
	// Multiple distinct licenses do not establish an AND/OR expression. Do not guess.
	value := ""
	for _, match := range coverage.Match {
		if match.IsURL {
			continue
		}
		if value != "" && value != match.ID {
			return "", nil
		}
		value = match.ID
	}
	if coverage.Percent < 90 {
		return "", nil
	}
	return value, nil
}
