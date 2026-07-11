package official

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/httpx"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const maxResponseBytes = 4 << 20

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
	Published     bool   `json:"published"`
	Skipped       bool   `json:"skipped"`
	Created       bool   `json:"created"`
	PackageID     string `json:"packageId"`
	Version       string `json:"version"`
	OnlineVersion string `json:"onlineVersion,omitempty"`
	UploadURL     string `json:"uploadUrl"`
	SHA256        string `json:"sha256"`
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
	token, err := request.Provider.Token(ctx)
	if err != nil {
		return Result{}, sanitizePublishError(err)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(publisher.BaseURL), "/")
	if baseURL == "" {
		baseURL = appstore.DefaultBaseURL
	}
	httpClient := httpx.NoRedirect(publisher.HTTPClient, 30*time.Second)
	client := appstore.New(appstore.Options{BaseURL: baseURL, HTTPClient: httpClient, Token: auth.StaticToken(token)})
	exists, err := client.CheckApplication(ctx, packageID)
	if err != nil {
		return Result{}, sanitizePublishError(err)
	}
	created := false
	if !exists {
		if !request.CreateIfMissing || application == nil {
			return Result{}, publishError(lpkgo.CodeNotFound, errors.New("official application does not exist"))
		}
		if err := client.CreateApplication(ctx, *application); err != nil {
			return Result{}, sanitizePublishError(err)
		}
		created = true
	}
	upload, err := uploadLPK(ctx, httpClient, baseURL, token, request.LPKPath, filename)
	if err != nil {
		return Result{}, err
	}
	uploadPackage := strings.TrimSpace(upload.Package)
	uploadVersion := strings.TrimSpace(upload.Version)
	uploadDigest := strings.ToLower(strings.TrimSpace(upload.SHA256))
	if uploadPackage != packageID || uploadVersion != version || uploadDigest != digest {
		return Result{}, publishError(lpkgo.CodeRemoteUnavailable, errors.New("official upload metadata does not match the verified LPK"))
	}
	if err := submitReview(ctx, httpClient, baseURL, token, upload, changelogs); err != nil {
		return Result{}, err
	}
	return Result{
		Published: true, Created: created, PackageID: uploadPackage, Version: uploadVersion,
		UploadURL: strings.TrimSpace(upload.URL), SHA256: uploadDigest,
	}, nil
}

func uploadLPK(ctx context.Context, client *http.Client, baseURL, token, filename, formFilename string) (appstore.UploadInfo, error) {
	file, err := os.Open(filename)
	if err != nil {
		return appstore.UploadInfo{}, publishError(lpkgo.CodeCommandFailed, errors.New("unable to open LPK for official publishing"))
	}
	defer file.Close()

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	done := make(chan error, 1)
	go func() {
		part, writeErr := multipartWriter.CreateFormFile("file", formFilename)
		if writeErr == nil {
			_, writeErr = io.Copy(part, contextReader{ctx: ctx, reader: file})
		}
		if writeErr == nil {
			writeErr = multipartWriter.Close()
		}
		if writeErr != nil {
			_ = writer.CloseWithError(writeErr)
		} else {
			writeErr = writer.Close()
		}
		done <- writeErr
	}()

	request, err := authenticatedRequest(ctx, http.MethodPost, baseURL+"/api/v3/developer/app/lpk/upload", token, reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-done
		return appstore.UploadInfo{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	body, requestErr := doRequest(client, request, "store.official.upload")
	if requestErr != nil {
		_ = reader.CloseWithError(requestErr)
	}
	writeErr := <-done
	if requestErr != nil {
		return appstore.UploadInfo{}, requestErr
	}
	if writeErr != nil {
		return appstore.UploadInfo{}, publishError(lpkgo.CodeCommandFailed, errors.New("unable to stream LPK for official publishing"))
	}
	var upload appstore.UploadInfo
	if err := json.Unmarshal(body, &upload); err != nil {
		return appstore.UploadInfo{}, publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform returned invalid upload metadata"))
	}
	if strings.TrimSpace(upload.Package) == "" || strings.TrimSpace(upload.Version) == "" || strings.TrimSpace(upload.URL) == "" || strings.TrimSpace(upload.SHA256) == "" {
		return appstore.UploadInfo{}, publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform returned incomplete upload metadata"))
	}
	return upload, nil
}

func submitReview(ctx context.Context, client *http.Client, baseURL, token string, upload appstore.UploadInfo, changelogs map[string]string) error {
	body := struct {
		Version struct {
			Package              string            `json:"package"`
			Name                 string            `json:"name"`
			IconPath             string            `json:"icon_path"`
			PackagePath          string            `json:"pkg_path"`
			PackageHash          string            `json:"pkg_hash"`
			UnsupportedPlatforms []string          `json:"unsupported_platforms"`
			MinOSVersion         string            `json:"min_os_version"`
			LPKSize              int64             `json:"lpk_size"`
			ImageSize            int64             `json:"image_size"`
			Changelogs           map[string]string `json:"changelogs"`
		} `json:"version"`
	}{}
	body.Version.Package = upload.Package
	body.Version.Name = upload.Version
	body.Version.IconPath = upload.IconPath
	body.Version.PackagePath = upload.URL
	body.Version.PackageHash = upload.SHA256
	body.Version.UnsupportedPlatforms = append([]string(nil), upload.UnsupportedPlatforms...)
	body.Version.MinOSVersion = upload.MinOSVersion
	body.Version.LPKSize = upload.LPKSize
	body.Version.ImageSize = upload.ImageSize
	body.Version.Changelogs = changelogs
	encoded, err := json.Marshal(body)
	if err != nil {
		return publishError(lpkgo.CodeInvalidArgument, errors.New("unable to encode official review request"))
	}
	target := baseURL + "/api/v3/developer/app/" + url.PathEscape(strings.TrimSpace(upload.Package)) + "/review/create"
	request, err := authenticatedRequest(ctx, http.MethodPost, target, token, strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	_, err = doRequest(client, request, "store.official.review")
	return err
}

func authenticatedRequest(ctx context.Context, method, target, token string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, publishError(lpkgo.CodeInvalidArgument, errors.New("unable to create official platform request"))
	}
	request.Header.Set("X-User-Token", token)
	request.AddCookie(&http.Cookie{Name: "userToken", Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	return request, nil
}

func doRequest(client *http.Client, request *http.Request, op string) ([]byte, error) {
	response, err := client.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, publishError(lpkgo.CodeCancelled, request.Context().Err())
		}
		return nil, publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform request failed"))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return nil, publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform response could not be read safely"))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		code := lpkgo.CodeRemoteUnavailable
		if response.StatusCode == http.StatusUnauthorized {
			code = lpkgo.CodeUnauthenticated
		} else if response.StatusCode == http.StatusForbidden {
			code = lpkgo.CodePermissionDenied
		}
		return nil, &lpkgo.Error{Code: code, Op: op, StatusCode: response.StatusCode, Retryable: response.StatusCode >= 500, Cause: errors.New("official platform rejected the request")}
	}
	return body, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
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
