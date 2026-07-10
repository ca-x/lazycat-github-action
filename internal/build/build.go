package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ca-x/lazycat-github-action/internal/lpkcheck"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/lint"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

type Request struct {
	Project         project.Info
	Version         string
	Tag             string
	Channel         string
	SourceDateEpoch int64
	Official        bool
	FailOnWarnings  bool
	RunBuildScript  bool
	Runner          toolkitbuild.CommandRunner
}

type Result struct {
	Path           string          `json:"path"`
	PackageID      string          `json:"packageId"`
	Version        string          `json:"version"`
	SHA256         string          `json:"sha256"`
	Size           int64           `json:"size"`
	TargetPlatform string          `json:"targetPlatform"`
	Warnings       []lpkgo.Warning `json:"warnings,omitempty"`
}

type Builder struct{}

var protectedBuildEnvironment = map[string]struct{}{
	"ACTIONS_CACHE_URL":              {},
	"ACTIONS_ID_TOKEN_REQUEST_TOKEN": {},
	"ACTIONS_ID_TOKEN_REQUEST_URL":   {},
	"ACTIONS_RESULTS_URL":            {},
	"ACTIONS_RUNTIME_TOKEN":          {},
	"GH_TOKEN":                       {},
	"GITHUB_ENV":                     {},
	"GITHUB_OUTPUT":                  {},
	"GITHUB_PATH":                    {},
	"GITHUB_STATE":                   {},
	"GITHUB_STEP_SUMMARY":            {},
	"GITHUB_TOKEN":                   {},
	"LAZYCAT_TOKEN":                  {},
	"LZC_CLI_TOKEN":                  {},
	"REGISTRY_PASSWORD":              {},
	"REGISTRY_USERNAME":              {},
}

type protectedRunner struct {
	base toolkitbuild.CommandRunner
}

func (runner protectedRunner) Run(ctx context.Context, command toolkitbuild.Command) error {
	environment := make(map[string]string, len(command.Env))
	for key, value := range command.Env {
		if _, protected := protectedBuildEnvironment[key]; protected {
			continue
		}
		environment[key] = value
	}
	command.Env = environment
	return runner.base.Run(ctx, command)
}

func (Builder) Build(ctx context.Context, request Request) (result Result, resultErr error) {
	if ctx == nil {
		return Result{}, errors.New("build LPK: nil context")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("build LPK: %w", err)
	}
	if strings.TrimSpace(request.Project.Root) == "" || strings.TrimSpace(request.Project.Output) == "" {
		return Result{}, errors.New("build LPK: project root and output are required")
	}
	if request.Version == "" {
		return Result{}, errors.New("build LPK: version is required")
	}
	if request.Project.Version != request.Version {
		return Result{}, fmt.Errorf("build LPK: package version %q does not match requested version %q", request.Project.Version, request.Version)
	}
	if request.Tag == "" {
		request.Tag = "v" + request.Version
	}

	output := filepath.Clean(request.Project.Output)
	outputDirectory := filepath.Dir(output)
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return Result{}, fmt.Errorf("create LPK output directory %q: %w", outputDirectory, err)
	}
	reserved, err := os.CreateTemp(outputDirectory, ".lazycat-action-*.lpk")
	if err != nil {
		return Result{}, fmt.Errorf("reserve temporary LPK in %q: %w", outputDirectory, err)
	}
	temporary := reserved.Name()
	if closeErr := reserved.Close(); closeErr != nil {
		_ = os.Remove(temporary)
		return Result{}, fmt.Errorf("close reserved LPK %q: %w", temporary, closeErr)
	}
	if err := os.Remove(temporary); err != nil {
		return Result{}, fmt.Errorf("prepare temporary LPK %q: %w", temporary, err)
	}
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporary)
		}
	}()

	environment := map[string]string{
		"LAZYCAT_VERSION":         request.Version,
		"LAZYCAT_TAG":             request.Tag,
		"LAZYCAT_CHANNEL":         request.Channel,
		"LAZYCAT_TARGET_OS":       platform.TargetOS,
		"LAZYCAT_TARGET_ARCH":     platform.TargetArch,
		"LAZYCAT_TARGET_PLATFORM": platform.TargetPlatform,
		"SOURCE_DATE_EPOCH":       strconv.FormatInt(request.SourceDateEpoch, 10),
	}
	runner := request.Runner
	if runner == nil {
		runner = toolkitbuild.ShellRunner{}
	}
	toolkitResult, err := toolkitbuild.BuildFile(ctx, temporary, toolkitbuild.Request{
		Root:               request.Project.Root,
		ConfigFile:         request.Project.BuildConfig,
		Environment:        environment,
		InheritEnvironment: true,
		RunBuildScript:     request.RunBuildScript,
		Runner:             protectedRunner{base: runner},
	})
	if err != nil {
		return Result{}, fmt.Errorf("build LPK with toolkit: %s: %w", rootCauseMessage(err), err)
	}

	reader, err := lpk.OpenFile(ctx, temporary)
	if err != nil {
		return Result{}, fmt.Errorf("reopen built LPK: %w", err)
	}
	effective, effectiveErr := reader.EffectiveManifest(ctx)
	if effectiveErr != nil {
		_ = reader.Close()
		return Result{}, fmt.Errorf("read built LPK manifest: %w", effectiveErr)
	}
	if effective.PackageInfo == nil {
		_ = reader.Close()
		return Result{}, errors.New("read built LPK manifest: package metadata is missing")
	}
	if effective.PackageInfo.Package != request.Project.PackageID {
		_ = reader.Close()
		return Result{}, fmt.Errorf("verify built LPK: package %q does not match expected %q", effective.PackageInfo.Package, request.Project.PackageID)
	}
	if effective.PackageInfo.Version != request.Version {
		_ = reader.Close()
		return Result{}, fmt.Errorf("verify built LPK: version %q does not match expected %q", effective.PackageInfo.Version, request.Version)
	}

	extractionParent, err := os.MkdirTemp("", "lazycat-action-lint-*")
	if err != nil {
		_ = reader.Close()
		return Result{}, fmt.Errorf("create LPK lint directory: %w", err)
	}
	defer os.RemoveAll(extractionParent)
	extractionRoot := filepath.Join(extractionParent, "root")
	extractErr := reader.Extract(ctx, extractionRoot)
	closeErr := reader.Close()
	if extractErr != nil || closeErr != nil {
		return Result{}, fmt.Errorf("extract built LPK for lint: %w", errors.Join(extractErr, closeErr))
	}

	var lintOptions []lint.Option
	if request.Official {
		lintOptions = append(lintOptions, lint.WithOfficial())
	}
	lintWarnings, err := lint.Package(ctx, os.DirFS(extractionRoot), lintOptions...)
	if err != nil {
		return Result{}, fmt.Errorf("lint built LPK: %w", err)
	}
	warnings := append([]lpkgo.Warning(nil), toolkitResult.Warnings...)
	warnings = append(warnings, lintWarnings...)
	if request.FailOnWarnings && len(lintWarnings) > 0 {
		profile := "basic lint"
		if request.Official {
			profile = "official lint"
		}
		return Result{}, fmt.Errorf("%s reported %d warning(s)", profile, len(lintWarnings))
	}

	digest, size, err := lpkcheck.HashFile(ctx, temporary)
	if err != nil {
		return Result{}, err
	}
	if err := os.Rename(temporary, output); err != nil {
		return Result{}, fmt.Errorf("publish LPK %q: %w", output, err)
	}
	if err := syncDirectory(outputDirectory); err != nil {
		return Result{}, err
	}
	return Result{
		Path:           output,
		PackageID:      effective.PackageInfo.Package,
		Version:        effective.PackageInfo.Version,
		SHA256:         digest,
		Size:           size,
		TargetPlatform: platform.TargetPlatform,
		Warnings:       warnings,
	}, nil
}

func rootCauseMessage(err error) string {
	root := err
	for {
		unwrapped := errors.Unwrap(root)
		if unwrapped == nil {
			return root.Error()
		}
		root = unwrapped
	}
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open LPK output directory for sync: %w", err)
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("sync LPK output directory: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}
