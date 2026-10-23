package action

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/licenselookup"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/releasenotes"
)

func changelogTemplate(input Input, cfg config.Config) string {
	if strings.TrimSpace(input.ChangelogTemplate) != "" {
		return input.ChangelogTemplate
	}
	return cfg.Release.ChangelogTemplate
}

func resolveChangelog(ctx context.Context, input Input, cfg config.Config, info project.Info, result Result, deps Dependencies) (string, error) {
	if strings.TrimSpace(input.Changelog) != "" {
		return input.Changelog, nil
	}
	repository := releasenotes.Repository(cfg.Release.GitHubRepository)
	if repository == "" {
		repository = licenselookup.Repository(info.Homepage)
	}
	if repository == "" {
		repository = releasenotes.Repository(deps.GitHubRepository)
	}
	data := releasenotes.Data{
		Version: result.Version, Tag: result.Tag, PackageID: info.PackageID,
		Name: info.Name, Homepage: info.Homepage, Repository: repository,
	}
	if cfg.Release.ShouldUseGitHubReleaseNotes() {
		if input.EventName == "release" && strings.TrimSpace(input.ReleaseNotes) != "" && len(input.ReleaseNotes) <= releasenotes.MaxChangelogBytes {
			if strings.TrimSpace(input.ChangelogTemplate) == "" && hasReleaseLPK(input.ReleaseAssets, result, info) {
				return input.ReleaseNotes, nil
			}
			data.ReleaseNotes, data.ReleaseURL = input.ReleaseNotes, input.ReleaseURL
		} else if repository != "" && deps.LookupReleaseNotes != nil {
			release, err := deps.LookupReleaseNotes(ctx, repository, []string{result.Tag, "v" + result.Version, result.Version})
			if err != nil {
				logger := deps.Logger
				if logger == nil {
					logger = slog.Default()
				}
				logger.Warn("GitHub release notes lookup failed; using changelog template fallback")
			} else {
				// Reuse final notes only when this repository already published the LPK.
				// A manual Release without that artifact still supplies raw template data.
				if strings.TrimSpace(input.ChangelogTemplate) == "" && strings.TrimSpace(release.Body) != "" && release.Tag == result.Tag && strings.EqualFold(repository, releasenotes.Repository(deps.GitHubRepository)) && hasReleaseLPK(release.AssetNames, result, info) {
					return release.Body, nil
				}
				data.ReleaseNotes, data.ReleaseURL = release.Body, release.URL
			}
		}
	}
	value, err := releasenotes.Render(changelogTemplate(input, cfg), data)
	if err != nil {
		return "", actionError(CodeConfigInvalid, "unable to render release changelog", err)
	}
	return value, nil
}

func hasReleaseLPK(assets []string, result Result, info project.Info) bool {
	versioned := result.PackageID + "-v" + result.Version + ".lpk"
	legacy := filepath.Base(info.Output)
	for _, name := range assets {
		if name == versioned || (strings.HasSuffix(legacy, ".lpk") && name == legacy) {
			return true
		}
	}
	return false
}
