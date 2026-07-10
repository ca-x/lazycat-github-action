package platformauth_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/platformauth"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestResolverUsesCredentialPrecedence(t *testing.T) {
	values := map[string]string{
		"LAZYCAT_TOKEN":    "primary-token",
		"LZC_CLI_TOKEN":    "fallback-token",
		"LAZYCAT_USERNAME": "developer@example.com",
		"LAZYCAT_PASSWORD": "password-value",
	}
	result, err := (platformauth.Resolver{
		LookupEnv: func(name string) (string, bool) {
			value, found := values[name]
			return value, found
		},
		Login: func(context.Context, auth.Credentials) (auth.Session, error) {
			t.Fatal("login must not run when LAZYCAT_TOKEN is set")
			return auth.Session{}, nil
		},
	}).Resolve(context.Background(), platformauth.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != platformauth.SourceLazyCatToken {
		t.Fatalf("source=%q", result.Source)
	}
	token, err := result.Provider.Token(context.Background())
	if err != nil || token != "primary-token" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestResolverSupportsFallbackLoginAndExplicitTokenFile(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		tokenFile bool
		want      string
		wantSrc   platformauth.Source
		wantErr   bool
	}{
		{name: "lzc cli token", env: map[string]string{"LZC_CLI_TOKEN": "cli-token"}, want: "cli-token", wantSrc: platformauth.SourceLZCCLIToken},
		{name: "account login", env: map[string]string{"LAZYCAT_USERNAME": "developer", "LAZYCAT_PASSWORD": " secret-password "}, want: "login-token", wantSrc: platformauth.SourceLogin},
		{name: "partial login", env: map[string]string{"LAZYCAT_USERNAME": "developer"}, wantErr: true},
		{name: "token file", tokenFile: true, want: "file-token", wantSrc: platformauth.SourceTokenFile},
		{name: "missing credentials", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := platformauth.Request{}
			if test.tokenFile {
				request.TokenFile = filepath.Join(t.TempDir(), "box-config.json")
				if err := os.WriteFile(request.TokenFile, []byte(`{"token":"file-token"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			loginCalls := 0
			result, err := (platformauth.Resolver{
				LookupEnv: func(name string) (string, bool) {
					value, found := test.env[name]
					return value, found
				},
				Login: func(_ context.Context, credentials auth.Credentials) (auth.Session, error) {
					loginCalls++
					if credentials.Username != "developer" || credentials.Password != " secret-password " {
						t.Fatalf("credentials=%#v", credentials)
					}
					return auth.Session{Token: "login-token"}, nil
				},
			}).Resolve(context.Background(), request)
			if test.wantErr {
				if err == nil || !errors.Is(err, lpkgo.ErrUnauthenticated) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Source != test.wantSrc {
				t.Fatalf("source=%q want=%q", result.Source, test.wantSrc)
			}
			token, tokenErr := result.Provider.Token(context.Background())
			if tokenErr != nil || token != test.want {
				t.Fatalf("token=%q err=%v", token, tokenErr)
			}
			wantLoginCalls := 0
			if test.wantSrc == platformauth.SourceLogin {
				wantLoginCalls = 1
			}
			if loginCalls != wantLoginCalls {
				t.Fatalf("login calls=%d want=%d", loginCalls, wantLoginCalls)
			}
		})
	}
}

func TestResolverRejectsUnsafeTokenFiles(t *testing.T) {
	root := t.TempDir()
	unsafe := filepath.Join(root, "unsafe.json")
	if err := os.WriteFile(unsafe, []byte(`{"token":"secret"}`), 0o622); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o622); err != nil {
		t.Fatal(err)
	}
	resolver := platformauth.Resolver{LookupEnv: func(string) (string, bool) { return "", false }}
	if _, err := resolver.Resolve(context.Background(), platformauth.Request{TokenFile: unsafe}); err == nil || !errors.Is(err, lpkgo.ErrPermissionDenied) {
		t.Fatalf("unsafe permissions err=%v", err)
	}
	readable := filepath.Join(root, "group-readable.json")
	if err := os.WriteFile(readable, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readable, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), platformauth.Request{TokenFile: readable}); err == nil || !errors.Is(err, lpkgo.ErrPermissionDenied) {
		t.Fatalf("group-readable permissions err=%v", err)
	}
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), platformauth.Request{TokenFile: link}); err == nil || !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("symlink err=%v", err)
	}
}

func TestResolverRedactsLoginFailureSecrets(t *testing.T) {
	const password = "must-never-appear"
	resolver := platformauth.Resolver{
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{"LAZYCAT_USERNAME": "developer", "LAZYCAT_PASSWORD": password}
			value, found := values[name]
			return value, found
		},
		Login: func(context.Context, auth.Credentials) (auth.Session, error) {
			return auth.Session{}, &lpkgo.Error{Code: lpkgo.CodeUnauthenticated, Op: "auth.login", Cause: errors.New("password=" + password)}
		},
	}
	_, err := resolver.Resolve(context.Background(), platformauth.Request{})
	if err == nil || strings.Contains(err.Error(), password) || !errors.Is(err, lpkgo.ErrUnauthenticated) {
		t.Fatalf("err=%v", err)
	}
}

func TestResolverDoesNotForwardPasswordAcrossRedirect(t *testing.T) {
	reached := false
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		reached = true
		body, _ := io.ReadAll(request.Body)
		forwarded = strings.Contains(string(body), "must-not-forward")
		_, _ = response.Write([]byte(`{"success":true,"data":{"token":"wrong-server-token"}}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	resolver := platformauth.Resolver{
		AccountBaseURL: origin.URL,
		HTTPClient:     origin.Client(),
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{"LAZYCAT_USERNAME": "developer", "LAZYCAT_PASSWORD": "must-not-forward"}
			value, found := values[name]
			return value, found
		},
	}
	_, err := resolver.Resolve(context.Background(), platformauth.Request{})
	if err == nil || reached || forwarded {
		t.Fatalf("err=%v reached=%v forwarded=%v", err, reached, forwarded)
	}
}

func TestProviderCachesResolvedLoginToken(t *testing.T) {
	loginCalls := 0
	provider := platformauth.NewProvider(platformauth.Resolver{
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{"LAZYCAT_USERNAME": "developer", "LAZYCAT_PASSWORD": "secret"}
			value, found := values[name]
			return value, found
		},
		Login: func(context.Context, auth.Credentials) (auth.Session, error) {
			loginCalls++
			return auth.Session{Token: "login-token"}, nil
		},
	}, func() string { return "" })
	for range 2 {
		token, err := provider.Token(context.Background())
		if err != nil || token != "login-token" {
			t.Fatalf("token=%q err=%v", token, err)
		}
	}
	if loginCalls != 1 {
		t.Fatalf("login calls=%d", loginCalls)
	}
}
