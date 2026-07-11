package imageflow_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/delivery"
	"github.com/ca-x/lazycat-github-action/internal/imageflow"
	"github.com/ca-x/lazycat-github-action/internal/manifestedit"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/registry"
	"github.com/ca-x/lazycat-github-action/internal/versioning"
	"github.com/lib-x/lzc-toolkit-go/appstore"
)

func TestFlowChecksAllImagesAndUpdatesOnlyChangedTarget(t *testing.T) {
	var logs bytes.Buffer
	registryClient := &fakeRegistry{bySource: map[string][]versioning.Candidate{
		"docker.io/library/postgres": {{Tag: "17.1.0", Digest: digest("d"), Created: created(1)}},
		"ghcr.io/acme/web":           {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}},
	}}
	deliverer := &fakeDeliverer{}
	var applied []manifestedit.Update
	flow := imageflow.Flow{
		Registry:  registryClient,
		Deliverer: deliverer,
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{
				{ID: "db", RuntimeRef: "docker.io/library/postgres:17.1.0", UpstreamRef: "docker.io/library/postgres:17.1.0"},
				{ID: "web", RuntimeRef: "ghcr.io/acme/web:v1.0.0", UpstreamRef: "ghcr.io/acme/web:v1.0.0"},
			}, nil
		},
		ApplyManifest: func(_ string, updates []manifestedit.Update) ([]manifestedit.Change, error) {
			applied = append(applied, updates...)
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: imageConfig(), Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != "2.0.0" || result.Channel != "stable" || len(result.Images) != 2 {
		t.Fatalf("result=%#v", result)
	}
	if len(applied) != 1 || applied[0].Target.ID != "web" {
		t.Fatalf("applied=%#v", applied)
	}
	if registryClient.calls != 2 || deliverer.calls != 2 {
		t.Fatalf("registry calls=%d delivery calls=%d", registryClient.calls, deliverer.calls)
	}
	for _, expected := range []string{"Docker image update started", "querying Docker image versions", "Docker image version selected", "Docker image delivery completed", "Docker image update completed"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("logs missing %q: %s", expected, logs.String())
		}
	}
}

func TestFlowExplicitNonDriverImageKeepsPackageVersion(t *testing.T) {
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"docker.io/library/postgres": {{Tag: "18.0.0", Digest: digest("d"), Created: created(1)}}}},
		Deliverer: &fakeDeliverer{},
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "db", RuntimeRef: "docker.io/library/postgres:17.1.0", UpstreamRef: "docker.io/library/postgres:17.1.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "db", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: imageConfig(), Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}, ImageID: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "1.0.0" || result.Channel != "" || len(result.Images) != 1 || result.Images[0].ID != "db" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowBlocksVersionSourceDowngradeBeforeDelivery(t *testing.T) {
	deliverer := &fakeDeliverer{}
	applied := 0
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Channel = "custom"
	cfg.Images[0].Sort = "created"
	cfg.Images[0].TagRegex = `^\d+\.\d+$`
	cfg.Images[0].VersionRegex = `^(?P<version>\d+\.\d+)$`
	cfg.Images[0].VersionTemplate = "{version}.0"
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "18.0", Digest: digest("e"), Created: created(11)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:19", UpstreamRef: "ghcr.io/acme/web:19.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			applied++
			return nil, nil
		},
	}
	_, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "19.0.0"}})
	if !errors.Is(err, imageflow.ErrVersionDowngrade) {
		t.Fatalf("err=%v", err)
	}
	if deliverer.calls != 0 || applied != 0 {
		t.Fatalf("deliveries=%d applied=%d", deliverer.calls, applied)
	}
}

func TestFlowAllowsExplicitVersionSourceDowngrade(t *testing.T) {
	deliverer := &fakeDeliverer{}
	cfg := imageConfig()
	cfg.Update.AllowDowngrade = true
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Channel = "custom"
	cfg.Images[0].Sort = "created"
	cfg.Images[0].TagRegex = `^\d+\.\d+$`
	cfg.Images[0].VersionRegex = `^(?P<version>\d+\.\d+)$`
	cfg.Images[0].VersionTemplate = "{version}.0"
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "18.0", Digest: digest("e"), Created: created(11)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:19", UpstreamRef: "ghcr.io/acme/web:19.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "19.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "18.0.0" || !result.Changed || deliverer.calls != 1 {
		t.Fatalf("deliveries=%d result=%#v", deliverer.calls, result)
	}
}

func TestFlowAllowsEqualVersionImageRefresh(t *testing.T) {
	deliverer := &fakeDeliverer{}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "v19.0.0", Digest: digest("f"), Created: created(11)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/web:old", UpstreamRef: "ghcr.io/acme/web:19.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "19.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "19.0.0" || !result.Changed || deliverer.calls != 1 {
		t.Fatalf("deliveries=%d result=%#v", deliverer.calls, result)
	}
}

func TestFlowDryRunDoesNotApplyAndReturnsCopyPlan(t *testing.T) {
	deliverer := &fakeDeliverer{copyResult: true}
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/acme/web:v1", UpstreamRef: "ghcr.io/acme/web:v1.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			t.Fatal("manifest applied during dry-run")
			return nil, nil
		},
	}
	config := imageConfig()
	config.Images = config.Images[1:]
	result, err := flow.Check(context.Background(), imageflow.Request{Config: config, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || deliverer.last.DryRun != true || result.Images[0].Copied {
		t.Fatalf("result=%#v request=%#v", result, deliverer.last)
	}
}

func TestFlowReturnsStructuredLazyCatCopyResult(t *testing.T) {
	deliverer := &fakeDeliverer{copyResult: true}
	flow := imageflow.Flow{
		Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {{Tag: "v2.0.0", Digest: digest("w"), Created: created(2)}}}},
		Deliverer: deliverer,
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "registry.lazycat.cloud/acme/web:v1", UpstreamRef: "ghcr.io/acme/web:v1.0.0"}}, nil
		},
		ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
			return []manifestedit.Change{{ID: "web", Changed: true}}, nil
		},
	}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Delivery.Mode = "lazycat"
	result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	image := result.Images[0]
	if !image.Copied || image.CopyResult == nil || image.CopyResult.Platform != "amd64" || !image.CopyResult.Finished {
		t.Fatalf("image=%#v", image)
	}
}

func TestFlowRefreshesMutableCreatedLazyCatImageAndRemainsIdempotent(t *testing.T) {
	candidate := versioning.Candidate{Tag: "nightly", Digest: "sha256:a1b2c3d4e5f6" + strings.Repeat("0", 52), Created: time.Date(2026, 7, 10, 15, 30, 20, 0, time.UTC)}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	cfg.Images[0].Channel = "nightly"
	cfg.Images[0].Sort = "created"
	cfg.Images[0].TagRegex = "^nightly$"
	cfg.Images[0].Delivery.Mode = "lazycat"
	cfg.Update.AllowDowngrade = true
	newVersion := "0.0.0-nightly.20260710153020.a1b2c3d4e5f6"

	tests := []struct {
		name           string
		projectVersion string
		currentRuntime string
		wantChanged    bool
	}{
		{name: "new digest address", projectVersion: "1.0.0", currentRuntime: "registry.lazycat.cloud/web:old-copy", wantChanged: true},
		{name: "same digest address", projectVersion: newVersion, currentRuntime: "registry.lazycat.cloud/web:nightly", wantChanged: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deliverer := &fakeDeliverer{copyResult: true}
			applied := 0
			flow := imageflow.Flow{
				Registry:  &fakeRegistry{bySource: map[string][]versioning.Candidate{"ghcr.io/acme/web": {candidate}}},
				Deliverer: deliverer,
				ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
					return []manifestedit.Current{{ID: "web", RuntimeRef: test.currentRuntime, UpstreamRef: "ghcr.io/acme/web:nightly"}}, nil
				},
				ApplyManifest: func(string, []manifestedit.Update) ([]manifestedit.Change, error) {
					applied++
					return []manifestedit.Change{{ID: "web", Changed: true}}, nil
				},
			}
			result, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml", Version: test.projectVersion}})
			if err != nil {
				t.Fatal(err)
			}
			if deliverer.calls != 1 || result.Changed != test.wantChanged {
				t.Fatalf("deliveries=%d result=%#v", deliverer.calls, result)
			}
			wantApplied := 0
			if test.wantChanged {
				wantApplied = 1
			}
			if applied != wantApplied {
				t.Fatalf("applied=%d wantChanged=%t", applied, test.wantChanged)
			}
		})
	}
}

func TestFlowValidatesManifestTargetsBeforeRegistryCalls(t *testing.T) {
	registryClient := &fakeRegistry{}
	flow := imageflow.Flow{
		Registry:  registryClient,
		Deliverer: &fakeDeliverer{},
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return nil, errors.New("service missing")
		},
	}
	if _, err := flow.Check(context.Background(), imageflow.Request{Config: imageConfig(), Project: project.Info{ManifestFile: "manifest.yml"}}); err == nil {
		t.Fatal("expected target validation failure")
	}
	if registryClient.calls != 0 {
		t.Fatalf("registry calls=%d", registryClient.calls)
	}
}

func TestFlowRejectsUnknownImageID(t *testing.T) {
	flow := imageflow.Flow{Registry: &fakeRegistry{}, Deliverer: &fakeDeliverer{}}
	if _, err := flow.Check(context.Background(), imageflow.Request{Config: imageConfig(), ImageID: "missing"}); err == nil {
		t.Fatal("expected unknown image ID failure")
	}
}

func TestFlowDoesNotMisclassifyRegistryAuthenticationFailureAsPlatformMissing(t *testing.T) {
	flow := imageflow.Flow{
		Registry:  errorRegistry{err: errors.New("unauthorized")},
		Deliverer: &fakeDeliverer{},
		ReadManifest: func(string, []manifestedit.Target) ([]manifestedit.Current, error) {
			return []manifestedit.Current{{ID: "web", RuntimeRef: "old", UpstreamRef: "old"}}, nil
		},
	}
	cfg := imageConfig()
	cfg.Images = cfg.Images[1:]
	_, err := flow.Check(context.Background(), imageflow.Request{Config: cfg, Project: project.Info{ManifestFile: "manifest.yml"}})
	if err == nil || errors.Is(err, imageflow.ErrPlatformNotFound) || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("err=%v", err)
	}
}

func TestFlowFixtureUpdatesExplicitWebServiceOnly(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "image-app")
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "testdata", "image-app"))); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, ".github", "lazycat-action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Project.Root = root
	info, err := project.Inspect(ctx, cfg.Project)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(info.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	flow := imageflow.Flow{
		Registry: &fakeRegistry{bySource: map[string][]versioning.Candidate{
			"ghcr.io/acme/example-web": {{Tag: "v2.0.0", Digest: digest("f"), Created: created(10)}},
		}},
		Deliverer: &fakeDeliverer{copyResult: true},
	}
	result, err := flow.Check(ctx, imageflow.Request{Config: cfg, Project: info})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(info.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != "2.0.0" || len(result.Images) != 1 || result.Images[0].ID != "web" {
		t.Fatalf("result=%#v", result)
	}
	if strings.Count(string(before), "image: postgres:17") != 1 || strings.Count(string(after), "image: postgres:17") != 1 {
		t.Fatalf("database service changed:\n%s", after)
	}
	if strings.Contains(string(after), "registry.lazycat.cloud/acme/example-web:old") || !strings.Contains(string(after), "# upstream: ghcr.io/acme/example-web:v2.0.0") {
		t.Fatalf("web service was not updated as expected:\n%s", after)
	}
}

type fakeRegistry struct {
	bySource map[string][]versioning.Candidate
	calls    int
}

type errorRegistry struct{ err error }

func (registryClient errorRegistry) Candidates(context.Context, string, ...registry.TagFilter) ([]versioning.Candidate, error) {
	return nil, registryClient.err
}

func (registryClient *fakeRegistry) Candidates(_ context.Context, source string, filters ...registry.TagFilter) ([]versioning.Candidate, error) {
	registryClient.calls++
	result := append([]versioning.Candidate(nil), registryClient.bySource[source]...)
	if len(filters) == 0 {
		return result, nil
	}
	filtered := result[:0]
	for _, candidate := range result {
		if filters[0].Include != nil && !filters[0].Include.MatchString(candidate.Tag) {
			continue
		}
		if filters[0].Exclude != nil && filters[0].Exclude.MatchString(candidate.Tag) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered, nil
}

type fakeDeliverer struct {
	calls      int
	last       delivery.Request
	copyResult bool
}

func (deliverer *fakeDeliverer) Deliver(_ context.Context, request delivery.Request) (delivery.Result, error) {
	deliverer.calls++
	deliverer.last = request
	runtime := request.SourceRef
	if request.Image.Delivery.Mode == "lazycat" {
		runtime = "registry.lazycat.cloud/" + request.Image.ID + ":" + request.Tag
	}
	result := delivery.Result{Mode: request.Image.Delivery.Mode, RuntimeRef: runtime}
	if deliverer.copyResult && !request.DryRun {
		copyResult := appstore.CopyImageResult{SourceImage: request.SourceRef, Platform: "amd64", LazyCatImage: runtime, Progress: appstore.CopyProgress{Finished: true}}
		result.Copied = true
		result.CopyResult = &copyResult
	}
	return result, nil
}

func imageConfig() config.Config {
	return config.Config{
		Update: config.Update{VersionSource: config.VersionSource{Type: config.VersionSourceImage, Image: "web"}},
		Images: []config.Image{
			{ID: "db", Target: "service", Service: "db", Source: "docker.io/library/postgres", Channel: "stable", Sort: "semver", VersionTemplate: "{version}", Delivery: config.Delivery{Mode: "direct"}},
			{ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web", Channel: "stable", Sort: "semver", VersionTemplate: "{version}", Delivery: config.Delivery{Mode: "direct"}},
		},
	}
}

func digest(character string) string { return "sha256:" + strings.Repeat(character, 64) }
func created(day int) time.Time      { return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC) }
