package official

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func TestRetryablePublishErrorRejectsForbiddenCodes(t *testing.T) {
	for _, code := range []lpkgo.Code{
		lpkgo.CodeInvalidArgument,
		lpkgo.CodeInvalidConfig,
		lpkgo.CodeInvalidManifest,
		lpkgo.CodeUnauthenticated,
		lpkgo.CodePermissionDenied,
		lpkgo.CodeNotFound,
		lpkgo.CodeCommandFailed,
		lpkgo.CodeIntegrityMismatch,
		lpkgo.CodeCancelled,
		lpkgo.CodeDeadlineExceeded,
	} {
		t.Run(string(code), func(t *testing.T) {
			err := &lpkgo.Error{Code: code, Retryable: true, Cause: errors.New("must not retry")}
			if retryablePublishError(err) {
				t.Fatalf("code %s was retryable", code)
			}
		})
	}
}

func TestRetryablePublishErrorRejectsStatus600EvenWhenFlagged(t *testing.T) {
	err := &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, StatusCode: 600, Retryable: true, Cause: errors.New("not HTTP 5xx")}
	if retryablePublishError(err) {
		t.Fatal("status 600 was retryable")
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)
	value := now.Add(45 * time.Second).Format(http.TimeFormat)
	delay, ok := parseRetryAfter(value, now)
	if !ok || delay != 45*time.Second {
		t.Fatalf("delay=%s ok=%v", delay, ok)
	}
}

func TestRetryAfterRecorderKeepsGreatestValidDelayAndIgnoresStatus600(t *testing.T) {
	responses := []*http.Response{
		{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"3"}}, Body: io.NopCloser(strings.NewReader(""))},
		{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"8"}}, Body: io.NopCloser(strings.NewReader(""))},
		{StatusCode: 600, Header: http.Header{"Retry-After": {"60"}}, Body: io.NopCloser(strings.NewReader(""))},
	}
	index := 0
	recorder := newRetryAfterRecorder(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		response := responses[index]
		index++
		return response, nil
	}))
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range responses {
		if _, err := recorder.RoundTrip(request); err != nil {
			t.Fatal(err)
		}
	}
	if delay := recorder.Delay(); delay != 8*time.Second {
		t.Fatalf("delay=%s", delay)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
