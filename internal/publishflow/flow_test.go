package publishflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/lpkcheck"
	"github.com/ca-x/lazycat-github-action/internal/platformauth"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/publishflow"
	"github.com/ca-x/lazycat-github-action/internal/store/official"
	private "github.com/ca-x/lazycat-github-action/internal/store/private"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

const artifactSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestFlowPublishesVerifiedArtifactToOfficialStore(t *testing.T) {
	var published official.Request
	flow := publishflow.Flow{
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
