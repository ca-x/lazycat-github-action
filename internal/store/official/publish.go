package official

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ca-x/lazycat-github-action/internal/config"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Request struct {
	Provider        auth.TokenProvider
	LPKPath         string
	FileName        string
	PackageID       string
	Version         string
	SHA256          string
	Changelog       string
	Locales         []string
	CreateIfMissing bool
	Application     config.OfficialApplication
	DefaultName     string
}

type Result struct {
	Published bool   `json:"published"`
	Created   bool   `json:"created"`
	PackageID string `json:"packageId"`
	Version   string `json:"version"`
	UploadURL string `json:"uploadUrl"`
	SHA256    string `json:"sha256"`
}

type Publisher struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (publisher Publisher) Publish(ctx context.Context, request Request) (Result, error) {
	if ctx == nil || request.Provider == nil {
		return Result{}, publishError(lpkgo.CodeInvalidArgument, errors.New("context and token provider are required"))
	}
	packageID := strings.TrimSpace(request.PackageID)
	version := strings.TrimSpace(request.Version)
	digest := strings.ToLower(strings.TrimSpace(request.SHA256))
	changelog := strings.TrimSpace(request.Changelog)
	if packageID == "" || version == "" || strings.TrimSpace(request.LPKPath) == "" || changelog == "" || !sha256Pattern.MatchString(digest) {
		return Result{}, publishError(lpkgo.CodeInvalidArgument, errors.New("LPK path, package, version, SHA256, and changelog are required"))
	}
	changelogs := make(map[string]string, len(request.Locales))
	for _, locale := range request.Locales {
		locale = strings.ToLower(strings.TrimSpace(locale))
		if locale == "" {
			continue
		}
		changelogs[locale] = changelog
	}
	if len(changelogs) == 0 {
		return Result{}, publishError(lpkgo.CodeInvalidArgument, errors.New("at least one changelog locale is required"))
	}
	file, err := os.Open(request.LPKPath)
	if err != nil {
		return Result{}, publishError(lpkgo.CodeCommandFailed, errors.New("unable to open LPK for official publishing"))
	}
	defer file.Close()

	var application *appstore.CreateApplicationRequest
	if request.CreateIfMissing {
		language := strings.TrimSpace(request.Application.Language)
		if language == "" {
			language = "zh"
		}
		name := strings.TrimSpace(request.Application.Name)
		if name == "" {
			name = strings.TrimSpace(request.DefaultName)
		}
		if name == "" {
			return Result{}, publishError(lpkgo.CodeInvalidArgument, errors.New("application name is required when create_if_missing is enabled"))
		}
		application = &appstore.CreateApplicationRequest{
			Package: packageID, Language: language, Name: name,
			Source: strings.TrimSpace(request.Application.Source), SourceAuthor: strings.TrimSpace(request.Application.SourceAuthor),
		}
	}
	filename := strings.TrimSpace(request.FileName)
	if filename == "" {
		filename = filepath.Base(request.LPKPath)
	}
	client := appstore.New(appstore.Options{BaseURL: publisher.BaseURL, HTTPClient: publisher.HTTPClient, Token: request.Provider})
	published, err := client.Publish(ctx, appstore.PublishRequest{
		Package: file, FileName: filename, Changelogs: changelogs,
		CreateIfMissing: request.CreateIfMissing, Application: application,
	})
	if err != nil {
		return Result{}, sanitizePublishError(err)
	}
	uploadPackage := strings.TrimSpace(published.Upload.Package)
	uploadVersion := strings.TrimSpace(published.Upload.Version)
	uploadDigest := strings.ToLower(strings.TrimSpace(published.Upload.SHA256))
	if uploadPackage != packageID || uploadVersion != version || uploadDigest != digest {
		return Result{}, publishError(lpkgo.CodeRemoteUnavailable, errors.New("official upload metadata does not match the verified LPK"))
	}
	return Result{
		Published: true, Created: published.Created, PackageID: uploadPackage, Version: uploadVersion,
		UploadURL: strings.TrimSpace(published.Upload.URL), SHA256: uploadDigest,
	}, nil
}

func sanitizePublishError(err error) error {
	var toolkitError *lpkgo.Error
	if errors.As(err, &toolkitError) {
		return &lpkgo.Error{
			Code: toolkitError.Code, Op: "store.official.publish", StatusCode: toolkitError.StatusCode,
			Retryable: toolkitError.Retryable, Cause: errors.New("official platform rejected the publish request"),
		}
	}
	return publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform publishing failed"))
}

func publishError(code lpkgo.Code, cause error) error {
	return &lpkgo.Error{Code: code, Op: "store.official.publish", Cause: fmt.Errorf("%w", cause)}
}
