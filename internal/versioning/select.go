package versioning

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/ca-x/lazycat-github-action/internal/appversion"
)

type Channel string

const (
	ChannelStable  Channel = "stable"
	ChannelBeta    Channel = "beta"
	ChannelNightly Channel = "nightly"
	ChannelCustom  Channel = "custom"
)

type Sort string

const (
	SortSemVer  Sort = "semver"
	SortCreated Sort = "created"
)

type Rule struct {
	Channel         Channel
	Sort            Sort
	TagRegex        *regexp.Regexp
	ExcludeRegex    *regexp.Regexp
	VersionRegex    *regexp.Regexp
	VersionTemplate string
}

type Candidate struct {
	Tag     string
	Digest  string
	Created time.Time
}

type Selection struct {
	Candidate Candidate
	Version   string
}

type ranked struct {
	candidate Candidate
	version   string
	semver    *semver.Version
}

func Select(rule Rule, candidates []Candidate) (Selection, error) {
	if err := validateRule(rule); err != nil {
		return Selection{}, err
	}
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if rule.TagRegex != nil && !rule.TagRegex.MatchString(candidate.Tag) {
			continue
		}
		if rule.ExcludeRegex != nil && rule.ExcludeRegex.MatchString(candidate.Tag) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return Selection{}, errors.New("no image tag matches the configured channel")
	}
	if rule.Channel == ChannelNightly {
		return selectNightly(filtered)
	}

	rankedCandidates := make([]ranked, 0, len(filtered))
	for _, candidate := range filtered {
		mapped, err := mapVersion(rule, candidate.Tag)
		if err != nil {
			if rule.Channel == ChannelStable || rule.Channel == ChannelBeta {
				continue
			}
			return Selection{}, err
		}
		parsed, err := semver.NewVersion(mapped)
		if err != nil {
			return Selection{}, fmt.Errorf("parse mapped version %q: %w", mapped, err)
		}
		switch rule.Channel {
		case ChannelStable:
			if parsed.Prerelease() != "" {
				continue
			}
		case ChannelBeta:
			if parsed.Prerelease() == "" {
				continue
			}
		}
		rankedCandidates = append(rankedCandidates, ranked{candidate: candidate, version: mapped, semver: parsed})
	}
	if len(rankedCandidates) == 0 {
		return Selection{}, errors.New("no valid image version matches the configured channel")
	}
	sort.SliceStable(rankedCandidates, func(i, j int) bool {
		if rule.Sort == SortCreated {
			if rankedCandidates[i].candidate.Created.Equal(rankedCandidates[j].candidate.Created) {
				return rankedCandidates[i].candidate.Tag > rankedCandidates[j].candidate.Tag
			}
			return rankedCandidates[i].candidate.Created.After(rankedCandidates[j].candidate.Created)
		}
		comparison := rankedCandidates[i].semver.Compare(rankedCandidates[j].semver)
		if comparison == 0 {
			return rankedCandidates[i].candidate.Tag > rankedCandidates[j].candidate.Tag
		}
		return comparison > 0
	})
	selected := rankedCandidates[0]
	return Selection{Candidate: selected.candidate, Version: selected.version}, nil
}

func validateRule(rule Rule) error {
	switch rule.Channel {
	case ChannelStable, ChannelBeta:
		if rule.Sort != SortSemVer {
			return fmt.Errorf("channel %q requires semver sorting", rule.Channel)
		}
	case ChannelNightly:
		if rule.Sort != SortCreated || rule.TagRegex == nil {
			return errors.New("nightly channel requires created sorting and tag regex")
		}
	case ChannelCustom:
		if rule.TagRegex == nil || (rule.Sort != SortSemVer && rule.Sort != SortCreated) {
			return errors.New("custom channel requires tag regex and explicit sorting")
		}
	default:
		return fmt.Errorf("unsupported channel %q", rule.Channel)
	}
	if rule.VersionRegex != nil && rule.VersionRegex.SubexpIndex("version") < 0 {
		return errors.New("version_regex must define a named version group")
	}
	return nil
}

func mapVersion(rule Rule, tag string) (string, error) {
	value := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if rule.VersionRegex != nil {
		matches := rule.VersionRegex.FindStringSubmatch(tag)
		if matches == nil {
			return "", fmt.Errorf("tag %q does not match version_regex", tag)
		}
		value = matches[rule.VersionRegex.SubexpIndex("version")]
	}
	template := rule.VersionTemplate
	if template == "" {
		template = "{version}"
	}
	value = strings.ReplaceAll(template, "{version}", value)
	if !appversion.IsValid(value) {
		return "", fmt.Errorf("mapped version %q from tag %q is not valid SemVer", value, tag)
	}
	return value, nil
}

func selectNightly(candidates []Candidate) (Selection, error) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Created.Equal(candidates[j].Created) {
			return candidates[i].Tag > candidates[j].Tag
		}
		return candidates[i].Created.After(candidates[j].Created)
	})
	selected := candidates[0]
	digest := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(selected.Digest)), "sha256:")
	if matched, _ := regexp.MatchString(`^[0-9a-f]{64}$`, digest); !matched {
		return Selection{}, fmt.Errorf("nightly digest %q must be a sha256 digest", selected.Digest)
	}
	if selected.Created.IsZero() {
		return Selection{}, errors.New("nightly image creation time is required")
	}
	version := fmt.Sprintf("0.0.0-nightly.%s.%s", selected.Created.UTC().Format("20060102150405"), digest[:12])
	return Selection{Candidate: selected, Version: version}, nil
}
