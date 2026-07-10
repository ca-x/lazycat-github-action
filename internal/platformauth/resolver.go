package platformauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/httpx"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/auth"
	"github.com/lib-x/lzc-toolkit-go/auth/tokenfile"
)

type Source string

const (
	SourceLazyCatToken Source = "lazycat-token"
	SourceLZCCLIToken  Source = "lzc-cli-token"
	SourceLogin        Source = "login"
	SourceTokenFile    Source = "token-file"
)

type Request struct {
	TokenFile string
}

type Result struct {
	Provider auth.TokenProvider
	Source   Source
}

type Resolver struct {
	AccountBaseURL string
	HTTPClient     *http.Client
	LookupEnv      func(string) (string, bool)
	Login          func(context.Context, auth.Credentials) (auth.Session, error)
	LoadFile       func(context.Context, string) (string, error)
}

func (resolver Resolver) Resolve(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, authError(lpkgo.CodeInvalidArgument, errors.New("context is required"))
	}
	if err := ctx.Err(); err != nil {
		return Result{}, authError(lpkgo.CodeCancelled, err)
	}
	lookup := resolver.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if token := environmentValue(lookup, "LAZYCAT_TOKEN"); token != "" {
		return tokenResult(token, SourceLazyCatToken), nil
	}
	if token := environmentValue(lookup, "LZC_CLI_TOKEN"); token != "" {
		return tokenResult(token, SourceLZCCLIToken), nil
	}

	username := environmentValue(lookup, "LAZYCAT_USERNAME")
	password, _ := lookup("LAZYCAT_PASSWORD")
	if username != "" || password != "" {
		if username == "" || password == "" {
			return Result{}, authError(lpkgo.CodeUnauthenticated, errors.New("both LAZYCAT_USERNAME and LAZYCAT_PASSWORD are required"))
		}
		login := resolver.Login
		if login == nil {
			login = auth.NewClient(auth.ClientOptions{
				BaseURL: resolver.AccountBaseURL, HTTPClient: httpx.NoRedirect(resolver.HTTPClient, 30*time.Second),
			}).Login
		}
		session, err := login(ctx, auth.Credentials{Username: username, Password: password})
		if err != nil {
			return Result{}, sanitizeAuthError(err, "LazyCat account login failed")
		}
		if strings.TrimSpace(session.Token) == "" {
			return Result{}, authError(lpkgo.CodeUnauthenticated, errors.New("LazyCat account login returned no token"))
		}
		return tokenResult(session.Token, SourceLogin), nil
	}

	if path := strings.TrimSpace(request.TokenFile); path != "" {
		resolved, err := validateTokenFile(path)
		if err != nil {
			return Result{}, err
		}
		load := resolver.LoadFile
		if load == nil {
			load = func(ctx context.Context, filename string) (string, error) {
				return (tokenfile.Store{Path: filename}).Load(ctx)
			}
		}
		token, err := load(ctx, resolved)
		if err != nil {
			return Result{}, sanitizeAuthError(err, "unable to load LazyCat token file")
		}
		if strings.TrimSpace(token) == "" {
			return Result{}, authError(lpkgo.CodeUnauthenticated, errors.New("LazyCat token file is empty"))
		}
		return tokenResult(token, SourceTokenFile), nil
	}
	return Result{}, authError(lpkgo.CodeUnauthenticated, errors.New("LazyCat credentials are unavailable"))
}

func tokenResult(token string, source Source) Result {
	return Result{Provider: auth.StaticToken(strings.TrimSpace(token)), Source: source}
}

func environmentValue(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}

func validateTokenFile(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", authError(lpkgo.CodeCommandFailed, errors.New("unable to resolve token file home directory"))
		}
		path = filepath.Join(home, path[2:])
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", authError(lpkgo.CodeInvalidArgument, errors.New("invalid token file path"))
	}
	current := filepath.VolumeName(resolved) + string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(resolved, current), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return "", authError(lpkgo.CodeCommandFailed, errors.New("unable to inspect token file"))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", authError(lpkgo.CodeInvalidArgument, errors.New("token file path must not contain symbolic links"))
		}
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", authError(lpkgo.CodeInvalidArgument, errors.New("token file must be a regular file"))
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", authError(lpkgo.CodePermissionDenied, errors.New("token file must not be accessible by group or other"))
	}
	return resolved, nil
}

func sanitizeAuthError(err error, message string) error {
	var toolkitError *lpkgo.Error
	if errors.As(err, &toolkitError) {
		return &lpkgo.Error{Code: toolkitError.Code, Op: "platformauth.resolve", StatusCode: toolkitError.StatusCode, Retryable: toolkitError.Retryable, Cause: errors.New(message)}
	}
	return authError(lpkgo.CodeUnauthenticated, errors.New(message))
}

func authError(code lpkgo.Code, cause error) error {
	return &lpkgo.Error{Code: code, Op: "platformauth.resolve", Cause: fmt.Errorf("%w", cause)}
}
