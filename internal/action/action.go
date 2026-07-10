package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	actionbuild "github.com/ca-x/lazycat-github-action/internal/build"
	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/yamledit"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const (
	CodeConfigInvalid      = "CONFIG_INVALID"
	CodeProjectUnsupported = "PROJECT_UNSUPPORTED"
	CodeVersionNotFound    = "VERSION_NOT_FOUND"
	CodeBuildFailed        = "BUILD_FAILED"
	CodeLPKInvalid         = "LPK_INVALID"
)

type Operation string

const (
	OperationAuto            Operation = "auto"
	OperationCheck           Operation = "check"
	OperationBuild           Operation = "build"
	OperationPublishOfficial Operation = "publish-official"
	OperationPublishPrivate  Operation = "publish-private"
)

type Input struct {
	Operation       Operation
	ConfigPath      string
	ImageID         string
	Version         string
	Tag             string
	Channel         string
	Changelog       string
	LPKPath         string
	DownloadURL     string
	EventName       string
	RefType         string
	RefName         string
	SourceDateEpoch int64
	DryRun          bool
}

type Result struct {
	Changed        bool            `json:"changed"`
	PackageID      string          `json:"packageId"`
	Version        string          `json:"version"`
	Tag            string          `json:"tag"`
	LPKPath        string          `json:"lpkPath"`
	SHA256         string          `json:"sha256"`
	DownloadURL    string          `json:"downloadUrl,omitempty"`
	ImageResults   json.RawMessage `json:"imageResults"`
	ResultFile     string          `json:"resultFile"`
	RunnerArch     string          `json:"runnerArch"`
	TargetPlatform string          `json:"targetPlatform"`
	Warnings       []lpkgo.Warning `json:"warnings,omitempty"`
}

type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Cause     error  `json:"-"`
}

func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}
	return err.Code + ": " + err.Message
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

type Dependencies struct {
	Host       platform.Host
	ResultDir  string
	LoadConfig func(string) (config.Config, error)
	Inspect    func(context.Context, config.Project) (project.Info, error)
	SetVersion func(string, string) (yamledit.Change, error)
	Build      func(context.Context, actionbuild.Request) (actionbuild.Result, error)
}

func DefaultDependencies(host platform.Host) Dependencies {
	builder := actionbuild.Builder{}
	return Dependencies{
		Host:       host,
		LoadConfig: config.Load,
		Inspect:    project.Inspect,
		SetVersion: yamledit.SetPackageVersion,
		Build:      builder.Build,
	}
}

func ResolveOperation(input Input) (Operation, error) {
	operation := input.Operation
	if operation == "" {
		operation = OperationAuto
	}
	if operation != OperationAuto {
		switch operation {
		case OperationCheck, OperationBuild, OperationPublishOfficial, OperationPublishPrivate:
			return operation, nil
		default:
			return "", fmt.Errorf("unsupported operation %q", operation)
		}
	}
	switch input.EventName {
	case "release", "workflow_dispatch":
		return OperationBuild, nil
	case "push":
		if input.RefType == "tag" || strings.HasPrefix(input.RefName, "v") {
			return OperationBuild, nil
		}
		return OperationBuild, nil
	case "schedule":
		return OperationCheck, nil
	default:
		return OperationBuild, nil
	}
}

func Run(ctx context.Context, input Input, dependencies Dependencies) (Result, error) {
	if ctx == nil {
		return Result{}, actionError(CodeConfigInvalid, "context is required", nil)
	}
	if err := validateDependencies(dependencies); err != nil {
		return Result{}, actionError(CodeConfigInvalid, "Action dependencies are incomplete", err)
	}
	operation, err := ResolveOperation(input)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, err.Error(), err)
	}
	if operation != OperationBuild {
		return Result{}, actionError(CodeProjectUnsupported, fmt.Sprintf("operation %q is not available in milestone 1", operation), nil)
	}
	if input.Version == "" {
		return Result{}, actionError(CodeVersionNotFound, "a SemVer version is required for build", nil)
	}
	if input.Tag == "" {
		input.Tag = "v" + input.Version
	}
	if input.ConfigPath == "" {
		input.ConfigPath = ".github/lazycat-action.yml"
	}

	cfg, err := dependencies.LoadConfig(input.ConfigPath)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to load Action configuration", err)
	}
	info, err := dependencies.Inspect(ctx, cfg.Project)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to inspect LazyCat project", err)
	}
	result := baseResult(input, dependencies.Host, info)
	result.Changed = info.Version != input.Version
	if input.DryRun {
		if err := writeResult(&result, resultDirectory(dependencies.ResultDir, info.Root)); err != nil {
			return Result{}, actionError(CodeBuildFailed, "unable to write dry-run result", err)
		}
		return result, nil
	}

	change, err := dependencies.SetVersion(info.PackageFile, input.Version)
	if err != nil {
		return Result{}, actionError(CodeConfigInvalid, "unable to update package.yml version", err)
	}
	result.Changed = change.Changed
	updated, err := dependencies.Inspect(ctx, cfg.Project)
	if err != nil {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeConfigInvalid, "unable to inspect updated LazyCat project", err)
	}
	built, err := dependencies.Build(ctx, actionbuild.Request{
		Project:         updated,
		Version:         input.Version,
		Tag:             input.Tag,
		Channel:         input.Channel,
		SourceDateEpoch: input.SourceDateEpoch,
		Official:        cfg.Stores.Official.Enabled,
		FailOnWarnings:  cfg.Stores.Official.Enabled,
		RunBuildScript:  cfg.Build.ShouldRunBuildScript(),
	})
	if err != nil {
		rollbackVersion(dependencies, info.PackageFile, change)
		return Result{}, actionError(CodeBuildFailed, "LPK build failed", err)
	}
	result.PackageID = built.PackageID
	result.LPKPath = built.Path
	result.SHA256 = built.SHA256
	result.TargetPlatform = built.TargetPlatform
	result.Warnings = built.Warnings
	if err := writeResult(&result, resultDirectory(dependencies.ResultDir, updated.Root)); err != nil {
		return Result{}, actionError(CodeBuildFailed, "unable to write Action result", err)
	}
	return result, nil
}

func baseResult(input Input, host platform.Host, info project.Info) Result {
	return Result{
		PackageID:      info.PackageID,
		Version:        input.Version,
		Tag:            input.Tag,
		DownloadURL:    input.DownloadURL,
		ImageResults:   json.RawMessage("[]"),
		RunnerArch:     host.Arch,
		TargetPlatform: platform.TargetPlatform,
	}
}

func validateDependencies(dependencies Dependencies) error {
	if dependencies.Host.OS == "" || dependencies.Host.Arch == "" || dependencies.LoadConfig == nil || dependencies.Inspect == nil || dependencies.SetVersion == nil || dependencies.Build == nil {
		return errors.New("host, loader, inspector, editor, and builder are required")
	}
	return nil
}

func rollbackVersion(dependencies Dependencies, filename string, change yamledit.Change) {
	if change.Changed && change.Old != "" {
		_, _ = dependencies.SetVersion(filename, change.Old)
	}
}

func resultDirectory(configured, root string) string {
	if configured != "" {
		return configured
	}
	return filepath.Join(root, ".lazycat-action")
}

func writeResult(result *Result, directory string) (resultErr error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return err
	}
	result.ResultFile = filepath.Join(absolute, "result.json")
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(absolute, ".result-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, result.ResultFile)
}

func actionError(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}
