package official

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/httpx"
	"github.com/cloudflare/backoff"
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
	Retry           config.OfficialRetry
	Logger          *slog.Logger
}

type Result struct {
	Published     bool   `json:"published"`
	Skipped       bool   `json:"skipped"`
	Created       bool   `json:"created"`
	PackageID     string `json:"packageId"`
	Version       string `json:"version"`
	OnlineVersion string `json:"onlineVersion,omitempty"`
	SkipReason    string `json:"skipReason,omitempty"`
	UploadURL     string `json:"uploadUrl"`
	SHA256        string `json:"sha256"`
}

type Publisher struct {
	BaseURL    string
	HTTPClient *http.Client
	NewDelay   func(max, initial time.Duration) func() time.Duration
	Wait       func(context.Context, time.Duration) error
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
	if err := PrecheckFile(ctx, request.LPKPath); err != nil {
		return Result{}, err
	}
	token, err := request.Provider.Token(ctx)
	if err != nil {
		return Result{}, sanitizePublishError(err)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(publisher.BaseURL), "/")
	if baseURL == "" {
		baseURL = appstore.DefaultBaseURL
	}
	maxAttempts := 1
	if request.Retry.Enabled && request.Retry.MaxAttempts > 1 {
		maxAttempts = request.Retry.MaxAttempts
	}
	httpClient := httpx.NoRedirect(publisher.HTTPClient, 30*time.Second)
	var retryAfter *retryAfterRecorder
	var nextDelay func() time.Duration
	var wait func(context.Context, time.Duration) error
	var logger *slog.Logger
	if maxAttempts > 1 {
		retryAfter = newRetryAfterRecorder(httpClient.Transport)
		httpClient.Transport = retryAfter
		policy := backoff.New(request.Retry.MaxDelay, request.Retry.InitialDelay)
		nextDelay = policy.Duration
		if publisher.NewDelay != nil {
			nextDelay = publisher.NewDelay(request.Retry.MaxDelay, request.Retry.InitialDelay)
		}
		wait = publisher.Wait
		if wait == nil {
			wait = waitForRetry
		}
		logger = request.Logger
		if logger == nil {
			logger = slog.Default()
		}
	}
	client := appstore.New(appstore.Options{BaseURL: baseURL, HTTPClient: httpClient, Token: auth.StaticToken(token)})
	created := false
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if retryAfter != nil {
			retryAfter.Reset()
		}
		result, err := publishAttempt(ctx, request, client, httpClient, baseURL, token, packageID, version, digest, filename, application, changelogs)
		created = created || result.Created
		if err == nil {
			result.Created = created
			return result, nil
		}
		if attempt == maxAttempts || !retryablePublishError(err) {
			return Result{}, err
		}
		delay := max(nextDelay(), retryAfter.Delay())
		if request.Retry.MaxDelay > 0 {
			delay = min(delay, request.Retry.MaxDelay)
		}
		var toolkitError *lpkgo.Error
		_ = errors.As(err, &toolkitError)
		attributes := []any{
			"store", "official",
			"attempt", attempt,
			"max_attempts", maxAttempts,
			"delay", delay,
			"code", toolkitError.Code,
		}
		if toolkitError.StatusCode != 0 {
			attributes = append(attributes, "status", toolkitError.StatusCode)
		}
		logger.Warn("official store publication retry scheduled", attributes...)
		if err := wait(ctx, delay); err != nil {
			return Result{}, publishError(lpkgo.CodeCancelled, err)
		}
	}
	return Result{}, publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform publishing failed"))
}

type retryAfterRecorder struct {
	base  http.RoundTripper
	mutex sync.Mutex
	delay time.Duration
}

func newRetryAfterRecorder(base http.RoundTripper) *retryAfterRecorder {
	if base == nil {
		base = http.DefaultTransport
	}
	return &retryAfterRecorder{base: base}
}

func (recorder *retryAfterRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := recorder.base.RoundTrip(request)
	if err != nil || response == nil || !isRetryableHTTPStatus(response.StatusCode) {
		return response, err
	}
	if delay, ok := parseRetryAfter(response.Header.Get("Retry-After"), time.Now()); ok {
		recorder.mutex.Lock()
		recorder.delay = max(recorder.delay, delay)
		recorder.mutex.Unlock()
	}
	return response, err
}

func (recorder *retryAfterRecorder) Reset() {
	recorder.mutex.Lock()
	recorder.delay = 0
	recorder.mutex.Unlock()
}

func (recorder *retryAfterRecorder) Delay() time.Duration {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return recorder.delay
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 || seconds > int64((time.Duration(1<<63-1))/time.Second) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	date, err := http.ParseTime(value)
	if err != nil || date.Before(now) {
		return 0, false
	}
	return date.Sub(now), true
}

func publishAttempt(
	ctx context.Context,
	request Request,
	client *appstore.Client,
	httpClient *http.Client,
	baseURL, token, packageID, version, digest, filename string,
	application *appstore.CreateApplicationRequest,
	changelogs map[string]string,
) (Result, error) {
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
		return Result{Created: created}, err
	}
	uploadPackage := strings.TrimSpace(upload.Package)
	uploadVersion := strings.TrimSpace(upload.Version)
	uploadDigest := strings.ToLower(strings.TrimSpace(upload.SHA256))
	if uploadPackage != packageID || uploadVersion != version || uploadDigest != digest {
		return Result{Created: created}, markNonRetryable(publishError(lpkgo.CodeRemoteUnavailable, errors.New("official upload metadata does not match the verified LPK")))
	}
	if err := submitReview(ctx, httpClient, baseURL, token, upload, changelogs); err != nil {
		return Result{Created: created}, err
	}
	return Result{
		Published: true, Created: created, PackageID: uploadPackage, Version: uploadVersion,
		UploadURL: strings.TrimSpace(upload.URL), SHA256: uploadDigest,
	}, nil
}

func retryablePublishError(err error) bool {
	var nonRetryable *nonRetryablePublishError
	if errors.As(err, &nonRetryable) {
		return false
	}
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) {
		return false
	}
	switch toolkitError.Code {
	case lpkgo.CodeInvalidArgument,
		lpkgo.CodeInvalidConfig,
		lpkgo.CodeInvalidManifest,
		lpkgo.CodeUnauthenticated,
		lpkgo.CodePermissionDenied,
		lpkgo.CodeNotFound,
		lpkgo.CodeCommandFailed,
		lpkgo.CodeIntegrityMismatch,
		lpkgo.CodeCancelled,
		lpkgo.CodeDeadlineExceeded:
		return false
	}
	if toolkitError.StatusCode >= http.StatusBadRequest && toolkitError.StatusCode < http.StatusInternalServerError && toolkitError.StatusCode != http.StatusTooManyRequests {
		return false
	}
	if toolkitError.StatusCode >= 600 {
		return false
	}
	return toolkitError.Retryable ||
		(toolkitError.Code == lpkgo.CodeRemoteUnavailable && toolkitError.StatusCode == 0) ||
		isRetryableHTTPStatus(toolkitError.StatusCode)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
		return appstore.UploadInfo{}, markNonRetryable(publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform returned invalid upload metadata")))
	}
	if strings.TrimSpace(upload.Package) == "" || strings.TrimSpace(upload.Version) == "" || strings.TrimSpace(upload.URL) == "" || strings.TrimSpace(upload.SHA256) == "" {
		return appstore.UploadInfo{}, markNonRetryable(publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform returned incomplete upload metadata")))
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
		if contextErr := request.Context().Err(); contextErr != nil {
			return nil, publishContextError(contextErr)
		}
		return nil, publishError(lpkgo.CodeRemoteUnavailable, errors.New("official platform request failed"))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if contextErr := request.Context().Err(); contextErr != nil {
		return nil, &lpkgo.Error{
			Code: contextErrorCode(contextErr), Op: op, StatusCode: response.StatusCode,
			Cause: contextErr,
		}
	}
	if err != nil {
		return nil, &lpkgo.Error{
			Code: statusErrorCode(response.StatusCode), Op: op, StatusCode: response.StatusCode,
			Retryable: isRetryableHTTPStatus(response.StatusCode) || response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices,
			Cause:     errors.New("official platform response could not be read safely"),
		}
	}
	if len(body) > maxResponseBytes {
		return nil, &lpkgo.Error{
			Code: statusErrorCode(response.StatusCode), Op: op, StatusCode: response.StatusCode,
			Retryable: isRetryableHTTPStatus(response.StatusCode), Cause: errors.New("official platform response could not be read safely"),
		}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &lpkgo.Error{
			Code: statusErrorCode(response.StatusCode), Op: op, StatusCode: response.StatusCode,
			Retryable: isRetryableHTTPStatus(response.StatusCode), Cause: errors.New("official platform rejected the request"),
		}
	}
	return body, nil
}

func statusErrorCode(status int) lpkgo.Code {
	switch status {
	case http.StatusUnauthorized:
		return lpkgo.CodeUnauthenticated
	case http.StatusForbidden:
		return lpkgo.CodePermissionDenied
	default:
		return lpkgo.CodeRemoteUnavailable
	}
}

func isRetryableHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError && status < 600
}

func publishContextError(err error) error {
	return publishError(contextErrorCode(err), err)
}

func contextErrorCode(err error) lpkgo.Code {
	if errors.Is(err, context.DeadlineExceeded) {
		return lpkgo.CodeDeadlineExceeded
	}
	return lpkgo.CodeCancelled
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

type nonRetryablePublishError struct {
	err error
}

func (err *nonRetryablePublishError) Error() string {
	return err.err.Error()
}

func (err *nonRetryablePublishError) Unwrap() error {
	return err.err
}

func markNonRetryable(err error) error {
	return &nonRetryablePublishError{err: err}
}
