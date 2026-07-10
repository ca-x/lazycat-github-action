package publishflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/lpkcheck"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/platformauth"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/store/official"
	private "github.com/ca-x/lazycat-github-action/internal/store/private"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

var (
	ErrPublishStrategyRequired = errors.New("publish strategy is required")
	ErrStoreDisabled           = errors.New("requested store is disabled")
	ErrReleaseAssetMissing     = errors.New("private publishing requires a GitHub Release Asset URL")
)

type Target string

const (
	TargetOfficial Target = "official"
	TargetPrivate  Target = "private"
)

type Request struct {
	Target      Target
	Config      config.Config
	Project     project.Info
	LPKPath     string
	Version     string
	Changelog   string
	DownloadURL string
	TokenFile   string
	DryRun      bool
}

type Result struct {
	Artifact lpkcheck.Result  `json:"-"`
	Official *official.Result `json:"official,omitempty"`
	Private  *private.Result  `json:"private,omitempty"`
}

type PrivatePublisher interface {
	Publish(context.Context, private.Request) (private.Result, error)
}

type Flow struct {
	Verify          func(context.Context, lpkcheck.Request) (lpkcheck.Result, error)
	ResolveAuth     func(context.Context, platformauth.Request) (platformauth.Result, error)
	PublishOfficial func(context.Context, official.Request) (official.Result, error)
	LookupEnv       func(string) (string, bool)
	NewPrivate      func(private.Options) (PrivatePublisher, error)
}

func Default() Flow {
	resolver := platformauth.Resolver{}
	officialPublisher := official.Publisher{}
	return Flow{
		Verify:          lpkcheck.File,
		ResolveAuth:     resolver.Resolve,
		PublishOfficial: officialPublisher.Publish,
		LookupEnv:       os.LookupEnv,
		NewPrivate: func(options private.Options) (PrivatePublisher, error) {
			return private.New(options)
		},
	}
}

func (flow Flow) Publish(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("publish stores: context is required")
	}
	if request.Config.Update.Strategy != config.StrategyPublish {
		return Result{}, ErrPublishStrategyRequired
	}
	switch request.Target {
	case TargetOfficial:
		if !request.Config.Stores.Official.Enabled {
			return Result{}, ErrStoreDisabled
		}
	case TargetPrivate:
		if !request.Config.Stores.Private.Enabled {
			return Result{}, ErrStoreDisabled
		}
		if strings.TrimSpace(request.DownloadURL) == "" {
			return Result{}, ErrReleaseAssetMissing
		}
	default:
		return Result{}, fmt.Errorf("unsupported publish target %q", request.Target)
	}
	version := strings.TrimSpace(request.Version)
	if version == "" {
		version = strings.TrimSpace(request.Project.Version)
	}
	if strings.TrimSpace(request.LPKPath) == "" || version == "" {
		return Result{}, errors.New("publish stores: LPK path and version are required")
	}
	verify := flow.Verify
	if verify == nil {
		return Result{}, errors.New("publish stores: LPK verifier is unavailable")
	}
	artifact, err := verify(ctx, lpkcheck.Request{
		ProjectRoot: request.Project.Root, Path: request.LPKPath,
		ExpectedPackageID: request.Project.PackageID, ExpectedVersion: version,
	})
	if err != nil {
		return Result{}, fmt.Errorf("verify publish artifact: %w", err)
	}
	if artifact.TargetPlatform != platform.TargetPlatform {
		return Result{}, fmt.Errorf("verify publish artifact: target %q does not match %q", artifact.TargetPlatform, platform.TargetPlatform)
	}
	result := Result{Artifact: artifact}
	switch request.Target {
	case TargetOfficial:
		return flow.publishOfficial(ctx, request, result)
	case TargetPrivate:
		return flow.publishPrivate(ctx, request, result)
	default:
		panic("validated publish target became invalid")
	}
}

func (flow Flow) publishOfficial(ctx context.Context, request Request, result Result) (Result, error) {
	changelog := strings.TrimSpace(request.Changelog)
	if changelog == "" {
		return Result{}, errors.New("official publishing requires a changelog")
	}
	input := official.Request{
		LPKPath: result.Artifact.Path, FileName: filepath.Base(result.Artifact.Path),
		PackageID: result.Artifact.PackageID, Version: result.Artifact.Version, SHA256: result.Artifact.SHA256,
		Changelog: changelog, Locales: request.Config.Stores.Official.Locales,
		CreateIfMissing: request.Config.Stores.Official.CreateIfMissing,
		Application:     request.Config.Stores.Official.Application, DefaultName: request.Project.Name,
	}
	if request.DryRun {
		result.Official = &official.Result{PackageID: input.PackageID, Version: input.Version, SHA256: input.SHA256}
		return result, nil
	}
	if flow.ResolveAuth == nil || flow.PublishOfficial == nil {
		return Result{}, errors.New("official publisher dependencies are unavailable")
	}
	resolved, err := flow.ResolveAuth(ctx, platformauth.Request{TokenFile: request.TokenFile})
	if err != nil {
		return Result{}, fmt.Errorf("resolve official credentials: %w", err)
	}
	input.Provider = resolved.Provider
	published, err := flow.PublishOfficial(ctx, input)
	if err != nil {
		return Result{}, fmt.Errorf("publish official store: %w", err)
	}
	result.Official = &published
	return result, nil
}

func (flow Flow) publishPrivate(ctx context.Context, request Request, result Result) (Result, error) {
	lookup := flow.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	appID := envValue(lookup, "APP_ID")
	name := strings.TrimSpace(request.Config.Stores.Private.Name)
	if name == "" {
		name = strings.TrimSpace(request.Project.Name)
	}
	if name == "" {
		name = result.Artifact.PackageID
	}
	summary := strings.TrimSpace(request.Config.Stores.Private.Summary)
	if summary == "" {
		summary = strings.TrimSpace(request.Project.Description)
	}
	if summary == "" {
		summary = name
	}
	input := private.Request{
		AppID: appID, PackageID: result.Artifact.PackageID, Name: name, Summary: summary,
		Version: result.Artifact.Version, Changelog: strings.TrimSpace(request.Changelog),
		DownloadURL: strings.TrimSpace(request.DownloadURL), SHA256: result.Artifact.SHA256,
	}
	if request.DryRun {
		result.Private = &private.Result{
			AppID: appID, PackageID: input.PackageID, Version: input.Version,
			DownloadURL: input.DownloadURL, SHA256: input.SHA256,
		}
		return result, nil
	}
	baseURL := envValue(lookup, "APPSTORE_URL")
	token := envValue(lookup, "APPSTORE_TOKEN")
	if baseURL == "" || token == "" {
		return Result{}, &lpkgo.Error{Code: lpkgo.CodeUnauthenticated, Op: "publishflow.private", Cause: errors.New("APPSTORE_URL and APPSTORE_TOKEN are required")}
	}
	if flow.NewPrivate == nil {
		return Result{}, errors.New("private publisher dependency is unavailable")
	}
	client, err := flow.NewPrivate(private.Options{BaseURL: baseURL, Token: token})
	if err != nil {
		return Result{}, fmt.Errorf("configure private store: %w", err)
	}
	published, err := client.Publish(ctx, input)
	if err != nil {
		return Result{}, fmt.Errorf("publish private store: %w", err)
	}
	result.Private = &published
	return result, nil
}

func envValue(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}
