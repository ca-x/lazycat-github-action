package publishflow_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/lpkcheck"
	"github.com/ca-x/lazycat-github-action/internal/platformauth"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/publishflow"
	"github.com/ca-x/lazycat-github-action/internal/store/official"
	private "github.com/ca-x/lazycat-github-action/internal/store/private"
	"github.com/ca-x/lazycat-github-action/internal/storelookup"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

const artifactSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestFlowPublishesVerifiedArtifactToOfficialStore(t *testing.T) {
	var logs bytes.Buffer
	var published official.Request
	flow := publishflow.Flow{
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) {
			return verifiedArtifact(), nil
		},
		ResolveAuth: func(context.Context, platformauth.Request) (platformauth.Result, error) {
			return platformauth.Result{Provider: auth.StaticToken("secret"), Source: platformauth.SourceLazyCatToken}, nil
		},
		PublishOfficial: func(_ context.Context, request official.Request) (official.Result, error) {
			published = request
			return official.Result{Published: true, PackageID: request.PackageID, Version: request.Version, SHA256: request.SHA256, UploadURL: "/upload.lpk"}, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Official.Enabled = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetOfficial, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes", TokenFile: "/tmp/token.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Official == nil || !result.Official.Published || result.Private != nil || published.PackageID != "cloud.lazycat.example" || published.SHA256 != artifactSHA || published.Changelog != "Release notes" || strings.Join(published.Locales, ",") != "zh,en" {
		t.Fatalf("result=%#v request=%#v", result, published)
	}
	for _, expected := range []string{"store publication started", "LPK publication artifact verified", "store publication completed", "store=official"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("logs missing %q: %s", expected, logs.String())
		}
	}
}

func TestFlowPublishesPrivateStoreWithEnvironmentAndMetadataDefaults(t *testing.T) {
	var options private.Options
	var published private.Request
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{"APPSTORE_URL": "https://store.example.com", "APPSTORE_TOKEN": "lcst_secret", "APP_ID": "42"}
			value, found := values[name]
			return value, found
		},
		NewPrivate: func(input private.Options) (publishflow.PrivatePublisher, error) {
			options = input
			return privatePublisherFunc(func(_ context.Context, request private.Request) (private.Result, error) {
				published = request
				return private.Result{Published: true, AppID: request.AppID, PackageID: request.PackageID, Version: request.Version, DownloadURL: request.DownloadURL, SHA256: request.SHA256}, nil
			}), nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Private == nil || result.Official != nil || options.BaseURL != "https://store.example.com" || options.Token != "lcst_secret" || published.AppID != "42" || published.Name != "Example" || published.Summary != "Example summary" || published.SHA256 != artifactSHA {
		t.Fatalf("result=%#v options=%#v request=%#v", result, options, published)
	}
}

func TestFlowSkipsOfficialPublishWhenOnlineVersionMatches(t *testing.T) {
	lookupCalls := 0
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		LookupVersion: func(_ context.Context, request storelookup.Request) (storelookup.Result, error) {
			lookupCalls++
			if request.Store != storelookup.StoreOfficial || request.PackageID != "cloud.lazycat.example" {
				t.Fatalf("lookup request=%#v", request)
			}
			return storelookup.Result{OnlineVersion: "1.2.3"}, nil
		},
		ResolveAuth: func(context.Context, platformauth.Request) (platformauth.Result, error) {
			t.Fatal("official authentication must not run for an equal online version")
			return platformauth.Result{}, nil
		},
		PublishOfficial: func(context.Context, official.Request) (official.Result, error) {
			t.Fatal("official publish must not run for an equal online version")
			return official.Result{}, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Official.Enabled = true
	cfg.Stores.Official.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetOfficial, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", Changelog: "Release notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if lookupCalls != 1 || result.Official == nil || result.Official.Published || !result.Official.Skipped || result.Official.OnlineVersion != "1.2.3" || result.Official.PackageID != "cloud.lazycat.example" || result.Official.SHA256 != artifactSHA {
		t.Fatalf("lookupCalls=%d result=%#v", lookupCalls, result)
	}
}

func TestFlowSkipsPrivatePublishWithSecretGroupCodes(t *testing.T) {
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{
				"APPSTORE_URL":              "https://store.example.com",
				"PRIVATE_STORE_GROUP_CODES": " ABC123, LATE23 ,,",
			}
			value, found := values[name]
			return value, found
		},
		LookupVersion: func(_ context.Context, request storelookup.Request) (storelookup.Result, error) {
			if request.Store != storelookup.StorePrivate || request.BaseURL != "https://store.example.com" || strings.Join(request.GroupCodes, ",") != "ABC123,LATE23" {
				t.Fatalf("lookup request=%#v", request)
			}
			return storelookup.Result{OnlineVersion: "1.2.3"}, nil
		},
		NewPrivate: func(private.Options) (publishflow.PrivatePublisher, error) {
			t.Fatal("private publisher must not be configured for an equal online version")
			return nil, nil
		},
	}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	cfg.Stores.Private.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Private == nil || result.Private.Published || !result.Private.Skipped || result.Private.OnlineVersion != "1.2.3" || result.Private.PackageID != "cloud.lazycat.example" || result.Private.SHA256 != artifactSHA {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowStoreLookupOutcomes(t *testing.T) {
	tests := []struct {
		name          string
		lookupVersion string
		lookupErr     error
		wantErr       bool
		wantPublished bool
		wantOnline    string
	}{
		{name: "different version publishes", lookupVersion: "1.2.2", wantPublished: true, wantOnline: "1.2.2"},
		{name: "not found publishes", lookupErr: lpkgo.ErrNotFound, wantPublished: true},
		{name: "lookup failure stops", lookupErr: &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			published := false
			flow := publishflow.Flow{
				Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
				LookupVersion: func(context.Context, storelookup.Request) (storelookup.Result, error) {
					return storelookup.Result{OnlineVersion: test.lookupVersion}, test.lookupErr
				},
				ResolveAuth: func(context.Context, platformauth.Request) (platformauth.Result, error) {
					return platformauth.Result{Provider: auth.StaticToken("secret")}, nil
				},
				PublishOfficial: func(_ context.Context, request official.Request) (official.Result, error) {
					published = true
					return official.Result{Published: true, PackageID: request.PackageID, Version: request.Version, SHA256: request.SHA256}, nil
				},
			}
			cfg := publishConfig()
			cfg.Stores.Official.Enabled = true
			cfg.Stores.Official.SkipIfVersionExists = true
			result, err := flow.Publish(context.Background(), publishflow.Request{
				Target: publishflow.TargetOfficial, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk", Version: "1.2.3", Changelog: "Release notes",
			})
			if (err != nil) != test.wantErr || published != test.wantPublished {
				t.Fatalf("published=%v result=%#v err=%v", published, result, err)
			}
			if !test.wantErr && result.Official.OnlineVersion != test.wantOnline {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestFlowRejectsUnsafePublishingStatesAndDryRunSkipsRemoteCalls(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config, *publishflow.Request)
		wantErr error
	}{
		{name: "pull strategy", mutate: func(cfg *config.Config, _ *publishflow.Request) { cfg.Update.Strategy = config.StrategyPull }, wantErr: publishflow.ErrPublishStrategyRequired},
		{name: "disabled store", mutate: func(cfg *config.Config, _ *publishflow.Request) { cfg.Stores.Private.Enabled = false }, wantErr: publishflow.ErrStoreDisabled},
		{name: "missing private URL", mutate: func(_ *config.Config, request *publishflow.Request) { request.DownloadURL = "" }, wantErr: publishflow.ErrReleaseAssetMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := publishConfig()
			cfg.Stores.Private.Enabled = true
			request := publishflow.Request{Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk", Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA}
			test.mutate(&request.Config, &request)
			_, err := (publishflow.Flow{}).Publish(context.Background(), request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("err=%v", err)
			}
		})
	}

	remoteCalled := false
	flow := publishflow.Flow{
		Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil },
		LookupVersion: func(context.Context, storelookup.Request) (storelookup.Result, error) {
			t.Fatal("dry-run must not query stores")
			return storelookup.Result{}, nil
		},
		LookupEnv: func(name string) (string, bool) {
			values := map[string]string{"APPSTORE_URL": "https://store.example.com", "APPSTORE_TOKEN": "lcst_secret"}
			value, found := values[name]
			return value, found
		},
		NewPrivate: func(private.Options) (publishflow.PrivatePublisher, error) {
			remoteCalled = true
			return nil, errors.New("must not create private client")
		},
	}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	cfg.Stores.Private.SkipIfVersionExists = true
	result, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk", ExpectedSHA256: artifactSHA, DryRun: true,
	})
	if err != nil || remoteCalled || result.Private == nil || result.Private.Published {
		t.Fatalf("result=%#v remoteCalled=%v err=%v", result, remoteCalled, err)
	}
}

func TestFlowRejectsReleaseAssetSHA256Mismatch(t *testing.T) {
	flow := publishflow.Flow{Verify: func(context.Context, lpkcheck.Request) (lpkcheck.Result, error) { return verifiedArtifact(), nil }}
	cfg := publishConfig()
	cfg.Stores.Private.Enabled = true
	_, err := flow.Publish(context.Background(), publishflow.Request{
		Target: publishflow.TargetPrivate, Config: cfg, Project: projectInfo(), LPKPath: "/repo/dist/app.lpk",
		Version: "1.2.3", DownloadURL: "https://github.com/acme/example/releases/download/v1.2.3/app.lpk",
		ExpectedSHA256: strings.Repeat("b", 64),
	})
	if !errors.Is(err, lpkgo.ErrIntegrityMismatch) {
		t.Fatalf("err=%v", err)
	}
}

type privatePublisherFunc func(context.Context, private.Request) (private.Result, error)

func (function privatePublisherFunc) Publish(ctx context.Context, request private.Request) (private.Result, error) {
	return function(ctx, request)
}

func verifiedArtifact() lpkcheck.Result {
	return lpkcheck.Result{Path: "/repo/dist/app.lpk", PackageID: "cloud.lazycat.example", Version: "1.2.3", SHA256: artifactSHA, Size: 123, TargetPlatform: "linux/amd64"}
}

func projectInfo() project.Info {
	return project.Info{Root: "/repo", PackageID: "cloud.lazycat.example", Version: "1.2.3", Name: "Example", Description: "Example summary", Output: "/repo/dist/app.lpk"}
}

func publishConfig() config.Config {
	return config.Config{
		Update: config.Update{Strategy: config.StrategyPublish},
		Stores: config.Stores{Official: config.OfficialStore{Locales: []string{"zh", "en"}}},
	}
}
