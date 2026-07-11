package versioning_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/versioning"
)

func TestSelectStableAndBeta(t *testing.T) {
	candidates := []versioning.Candidate{
		{Tag: "v1.9.0", Digest: digest("1"), Created: at(1)},
		{Tag: "v2.0.0-beta.1", Digest: digest("2"), Created: at(2)},
		{Tag: "v2.0.0-rc.1", Digest: digest("3"), Created: at(3)},
		{Tag: "v2.0.0", Digest: digest("4"), Created: at(4)},
	}
	tests := []struct {
		channel versioning.Channel
		wantTag string
		wantVer string
	}{
		{channel: versioning.ChannelStable, wantTag: "v2.0.0", wantVer: "2.0.0"},
		{channel: versioning.ChannelBeta, wantTag: "v2.0.0-rc.1", wantVer: "2.0.0-rc.1"},
	}
	for _, test := range tests {
		selection, err := versioning.Select(versioning.Rule{Channel: test.channel, Sort: versioning.SortSemVer, VersionTemplate: "{version}"}, candidates)
		if err != nil {
			t.Fatal(err)
		}
		if selection.Candidate.Tag != test.wantTag || selection.Version != test.wantVer {
			t.Fatalf("channel=%q selection=%#v", test.channel, selection)
		}
	}
}

func TestSelectNightlyUsesAMD64CreationTimeAndDigest(t *testing.T) {
	selection, err := versioning.Select(versioning.Rule{
		Channel:      versioning.ChannelNightly,
		Sort:         versioning.SortCreated,
		TagRegex:     regexp.MustCompile(`^nightly`),
		ExcludeRegex: regexp.MustCompile(`arm`),
	}, []versioning.Candidate{
		{Tag: "nightly-arm", Digest: digest("a"), Created: time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)},
		{Tag: "nightly", Digest: "sha256:a1b2c3d4e5f6" + strings.Repeat("0", 52), Created: time.Date(2026, 7, 10, 15, 30, 20, 0, time.UTC)},
		{Tag: "nightly-old", Digest: digest("b"), Created: time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Candidate.Tag != "nightly" || selection.Version != "0.0.0-nightly.20260710153020.a1b2c3d4e5f6" {
		t.Fatalf("selection=%#v", selection)
	}
}

func TestSelectCustomByMappedSemVerAndCreated(t *testing.T) {
	rule := versioning.Rule{
		Channel:         versioning.ChannelCustom,
		Sort:            versioning.SortSemVer,
		TagRegex:        regexp.MustCompile(`^release-`),
		VersionRegex:    regexp.MustCompile(`^release-(?P<version>\d+\.\d+\.\d+)$`),
		VersionTemplate: "{version}",
	}
	selection, err := versioning.Select(rule, []versioning.Candidate{
		{Tag: "release-1.9.0", Digest: digest("1"), Created: at(4)},
		{Tag: "release-2.0.0", Digest: digest("2"), Created: at(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Version != "2.0.0" {
		t.Fatalf("selection=%#v", selection)
	}

	rule.Sort = versioning.SortCreated
	selection, err = versioning.Select(rule, []versioning.Candidate{
		{Tag: "release-1.9.0", Digest: digest("1"), Created: at(4)},
		{Tag: "release-2.0.0", Digest: digest("2"), Created: at(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Version != "1.9.0" {
		t.Fatalf("selection=%#v", selection)
	}
}

func TestSelectExpandsNamedVersionTemplateGroups(t *testing.T) {
	rule := versioning.Rule{
		Channel:         versioning.ChannelCustom,
		Sort:            versioning.SortCreated,
		TagRegex:        regexp.MustCompile(`^\d{8}\.\d+$`),
		VersionRegex:    regexp.MustCompile(`^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$`),
		VersionTemplate: "{version}.{build}.0",
	}
	selection, err := versioning.Select(rule, []versioning.Candidate{{Tag: "20260603.01", Digest: digest("1"), Created: at(1)}})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Version != "20260603.1.0" {
		t.Fatalf("selection=%#v", selection)
	}

	rule.VersionTemplate = "{version}.{missing}.0"
	_, err = versioning.Select(rule, []versioning.Candidate{{Tag: "20260603.01", Digest: digest("1"), Created: at(1)}})
	if err == nil || !strings.Contains(err.Error(), "unresolved version template placeholder") {
		t.Fatalf("err=%v", err)
	}
}

func TestSelectRejectsInvalidRulesAndCandidates(t *testing.T) {
	tests := []struct {
		name       string
		rule       versioning.Rule
		candidates []versioning.Candidate
	}{
		{name: "empty", rule: versioning.Rule{Channel: versioning.ChannelStable, Sort: versioning.SortSemVer}},
		{name: "missing named group", rule: versioning.Rule{Channel: versioning.ChannelCustom, Sort: versioning.SortSemVer, TagRegex: regexp.MustCompile(`.`), VersionRegex: regexp.MustCompile(`(.*)`)}, candidates: []versioning.Candidate{{Tag: "1.2.3", Digest: digest("1")}}},
		{name: "invalid mapped semver", rule: versioning.Rule{Channel: versioning.ChannelCustom, Sort: versioning.SortSemVer, TagRegex: regexp.MustCompile(`.`), VersionRegex: regexp.MustCompile(`(?P<version>.*)`), VersionTemplate: "v{version}"}, candidates: []versioning.Candidate{{Tag: "1.2.3", Digest: digest("1")}}},
		{name: "short nightly digest", rule: versioning.Rule{Channel: versioning.ChannelNightly, Sort: versioning.SortCreated, TagRegex: regexp.MustCompile(`.`)}, candidates: []versioning.Candidate{{Tag: "nightly", Digest: "sha256:abc", Created: at(1)}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := versioning.Select(test.rule, test.candidates); err == nil {
				t.Fatal("expected selection to fail")
			}
		})
	}
}

func digest(character string) string { return "sha256:" + strings.Repeat(character, 64) }
func at(day int) time.Time           { return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC) }
