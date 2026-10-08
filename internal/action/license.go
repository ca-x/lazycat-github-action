package action

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/licenselookup"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/yamledit"
)

// License synchronization is best effort and runs only after publication gates pass.
func syncLicense(ctx context.Context, input Input, cfg config.Config, info project.Info, deps Dependencies, result *Result) {
	if input.DryRun || !cfg.Update.ShouldSyncLicense() || deps.LookupLicense == nil || licenselookup.Repository(info.Homepage) == "" {
		return
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	value, err := deps.LookupLicense(ctx, info.Homepage)
	if err != nil {
		logger.Warn("GitHub license lookup failed; continuing without license synchronization")
		return
	}
	if value == "" || value == "NOASSERTION" || value == "NONE" {
		logger.Info("GitHub license was not identified; retaining package license")
		return
	}
	if value == info.License {
		return
	}
	original, err := os.ReadFile(info.PackageFile)
	if err != nil {
		logger.Warn("unable to read package license; continuing")
		return
	}
	// Prepare the license-only snapshot before touching the working package.
	directory, err := filepath.Abs(resultDirectory(deps.ResultDir, info.Root))
	if err != nil {
		logger.Warn("unable to resolve license snapshot directory; continuing")
		return
	}
	if stat, statErr := os.Lstat(directory); statErr == nil && stat.Mode()&os.ModeSymlink != 0 {
		logger.Warn("license snapshot directory is a symbolic link; continuing")
		return
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		logger.Warn("unable to create license snapshot directory; continuing")
		return
	}
	snapshotFile, err := os.CreateTemp(directory, "license-package-*.yml")
	if err != nil {
		logger.Warn("unable to create license snapshot; continuing")
		return
	}
	snapshot := snapshotFile.Name()
	_, writeErr := snapshotFile.Write(original)
	closeErr := snapshotFile.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(snapshot)
		logger.Warn("unable to save license snapshot; continuing")
		return
	}
	if _, err := yamledit.SetPackageLicense(snapshot, value); err != nil {
		_ = os.Remove(snapshot)
		logger.Warn("unable to prepare license snapshot; continuing")
		return
	}
	change, err := yamledit.SetPackageLicense(info.PackageFile, value)
	if err != nil || !change.Changed {
		_ = os.Remove(snapshot)
		logger.Warn("package license unchanged or update failed; continuing")
		return
	}
	result.LicenseChanged = true
	result.License = value
	result.LicenseFile = snapshot
	result.Changed = true
	logger.Info("package license synchronized from GitHub", "license", value)
}
