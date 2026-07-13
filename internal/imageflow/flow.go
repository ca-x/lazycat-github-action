package imageflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/delivery"
	"github.com/ca-x/lazycat-github-action/internal/manifestedit"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/registry"
	"github.com/ca-x/lazycat-github-action/internal/versioning"
	"github.com/lib-x/lzc-toolkit-go/appstore"
)

var (
	ErrVersionNotFound  = errors.New("image version not found")
	ErrVersionDowngrade = errors.New("image version downgrade blocked")
	ErrPlatformNotFound = errors.New("image platform not found")
	ErrDeliveryFailed   = errors.New("image delivery failed")
)

type Registry interface {
	CandidatesForTarget(context.Context, string, platform.Target, ...registry.TagFilter) ([]versioning.Candidate, error)
}

type Deliverer interface {
	Deliver(context.Context, delivery.Request) (delivery.Result, error)
}

type Request struct {
	Config     config.Config
	Project    project.Info
	ImageID    string
	DryRun     bool
	OnProgress func(string, appstore.CopyProgress)
}

type LayerProgress struct {
	Hash     string `json:"hash"`
	Progress int    `json:"progress"`
}

type CopyResult struct {
	SourceImage  string          `json:"sourceImage"`
	Platform     string          `json:"platform"`
	LazyCatImage string          `json:"lazyCatImage"`
	Finished     bool            `json:"finished"`
	Layers       []LayerProgress `json:"layers,omitempty"`
}

type ImageResult struct {
	ID           string      `json:"id"`
	Target       string      `json:"target"`
	Service      string      `json:"service,omitempty"`
	Platform     string      `json:"platform"`
	Tag          string      `json:"tag"`
	SourceRef    string      `json:"sourceRef"`
	SourceDigest string      `json:"sourceDigest"`
	DeliveryMode string      `json:"deliveryMode"`
	DeliveredRef string      `json:"deliveredRef"`
	Copied       bool        `json:"copied"`
	CopyResult   *CopyResult `json:"copyResult,omitempty"`
}

type Result struct {
	Changed bool          `json:"changed"`
	Version string        `json:"version"`
	Channel string        `json:"channel,omitempty"`
	Images  []ImageResult `json:"images"`
}

type Flow struct {
	Registry      Registry
	Deliverer     Deliverer
	ReadManifest  func(string, []manifestedit.Target) ([]manifestedit.Current, error)
	ApplyManifest func(string, []manifestedit.Update) ([]manifestedit.Change, error)
	Logger        *slog.Logger
}

func (flow Flow) Check(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("image check context is required")
	}
	if flow.Registry == nil || flow.Deliverer == nil {
		return Result{}, errors.New("image check requires Registry and delivery adapters")
	}
	selected, err := selectImages(request.Config.Images, request.ImageID)
	if err != nil {
		return Result{}, err
	}
	logger := flow.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	target := request.Config.Project.Target()
	logger.Info("Docker image update started", "images", len(selected), "dry_run", request.DryRun, "target", target.Platform())
	readManifest := flow.ReadManifest
	if readManifest == nil {
		readManifest = manifestedit.Read
	}
	applyManifest := flow.ApplyManifest
	if applyManifest == nil {
		applyManifest = manifestedit.Apply
	}
	targets := make([]manifestedit.Target, 0, len(selected))
	for _, image := range selected {
		targets = append(targets, imageTarget(image))
	}
	currentValues, err := readManifest(request.Project.ManifestFile, targets)
	if err != nil {
		return Result{}, fmt.Errorf("validate manifest image targets: %w", err)
	}
	currentByID := make(map[string]manifestedit.Current, len(currentValues))
	for _, current := range currentValues {
		currentByID[current.ID] = current
	}
	for _, image := range selected {
		if _, exists := currentByID[image.ID]; !exists {
			return Result{}, fmt.Errorf("manifest reader did not return image target %q", image.ID)
		}
	}

	result := Result{Version: request.Project.Version, Images: make([]ImageResult, 0, len(selected))}
	updates := make([]manifestedit.Update, 0, len(selected))
	for _, image := range selected {
		logger.Info("querying Docker image versions", "image_id", image.ID, "source", image.Source, "channel", image.Channel, "sort", image.Sort)
		rule, filter, err := imageRule(image)
		if err != nil {
			return Result{}, err
		}
		candidates, err := flow.Registry.CandidatesForTarget(ctx, image.Source, target, filter)
		if err != nil {
			if errors.Is(err, registry.ErrPlatformNotFound) {
				return Result{}, fmt.Errorf("%w: inspect %q: %v", ErrPlatformNotFound, image.ID, err)
			}
			return Result{}, fmt.Errorf("inspect image %q: %w", image.ID, err)
		}
		logger.Info("Docker image versions received", "image_id", image.ID, "candidates", len(candidates))
		selection, err := versioning.Select(rule, candidates)
		if err != nil {
			return Result{}, fmt.Errorf("%w for %q: %v", ErrVersionNotFound, image.ID, err)
		}
		logger.Info("Docker image version selected", "image_id", image.ID, "tag", selection.Candidate.Tag, "version", selection.Version, "digest", selection.Candidate.Digest, "platform", target.Platform())
		if request.Config.Update.VersionSource.Type == config.VersionSourceImage && request.Config.Update.VersionSource.Image == image.ID && !request.Config.Update.AllowDowngrade {
			currentVersion, currentErr := semver.StrictNewVersion(strings.TrimSpace(request.Project.Version))
			selectedVersion, selectedErr := semver.StrictNewVersion(selection.Version)
			if currentErr != nil || selectedErr != nil {
				return Result{}, fmt.Errorf("compare selected image version %q with current application version %q: %w", selection.Version, request.Project.Version, errors.Join(currentErr, selectedErr))
			}
			if selectedVersion.LessThan(currentVersion) {
				return Result{}, fmt.Errorf("%w for %q: selected %s is lower than current %s", ErrVersionDowngrade, image.ID, selection.Version, request.Project.Version)
			}
		}
		sourceRef := image.Source + ":" + selection.Candidate.Tag
		current := currentByID[image.ID]
		deliveryRequest := delivery.Request{
			Image: image, Tag: selection.Candidate.Tag, SourceRef: sourceRef, SourceDigest: selection.Candidate.Digest, Target: target, DryRun: request.DryRun,
		}
		if request.OnProgress != nil {
			deliveryRequest.OnProgress = func(progress appstore.CopyProgress) { request.OnProgress(image.ID, progress) }
		}
		var progressMu sync.Mutex
		layerProgress := map[string]int{}
		previousProgress := deliveryRequest.OnProgress
		deliveryRequest.OnProgress = func(progress appstore.CopyProgress) {
			if previousProgress != nil {
				previousProgress(progress)
			}
			progressMu.Lock()
			defer progressMu.Unlock()
			for _, layer := range progress.Layers {
				last := layerProgress[layer.Hash]
				if layer.Progress == 100 || layer.Progress >= last+25 {
					layerProgress[layer.Hash] = layer.Progress
					logger.Info("Docker image layer progress", "image_id", image.ID, "layer", layer.Hash, "progress", layer.Progress)
				}
			}
			if progress.Finished {
				logger.Info("Docker image copy stream completed", "image_id", image.ID)
			}
		}
		logger.Info("Docker image delivery started", "image_id", image.ID, "mode", image.Delivery.Mode, "source", sourceRef)

		needsUpdate := false
		var delivered delivery.Result
		switch image.Delivery.Mode {
		case "direct", "mirror":
			delivered, err = flow.Deliverer.Deliver(ctx, deliveryRequest)
			if err != nil {
				return Result{}, fmt.Errorf("%w for %q: %v", ErrDeliveryFailed, image.ID, err)
			}
			needsUpdate = current.UpstreamRef != sourceRef || current.RuntimeRef != delivered.RuntimeRef
		case "lazycat":
			shouldDeliver := current.UpstreamRef != sourceRef || current.RuntimeRef == "" || !strings.HasPrefix(current.RuntimeRef, "registry.lazycat.cloud/") || image.Sort == "created" || image.Sort == "updated"
			if shouldDeliver {
				delivered, err = flow.Deliverer.Deliver(ctx, deliveryRequest)
				if err != nil {
					return Result{}, fmt.Errorf("%w for %q: %v", ErrDeliveryFailed, image.ID, err)
				}
			} else {
				delivered = delivery.Result{Mode: "lazycat", RuntimeRef: current.RuntimeRef}
			}
			if request.DryRun {
				needsUpdate = shouldDeliver
			} else {
				needsUpdate = current.UpstreamRef != sourceRef || current.RuntimeRef != delivered.RuntimeRef
			}
		default:
			return Result{}, fmt.Errorf("unsupported delivery mode %q", image.Delivery.Mode)
		}
		if delivered.RuntimeRef == "" {
			delivered.RuntimeRef = current.RuntimeRef
		}
		logger.Info("Docker image delivery completed", "image_id", image.ID, "mode", image.Delivery.Mode, "copied", delivered.Copied, "runtime_ref", delivered.RuntimeRef)
		if needsUpdate && !request.DryRun && delivered.RuntimeRef == "" {
			return Result{}, fmt.Errorf("%w for %q: delivery returned an empty runtime reference", ErrDeliveryFailed, image.ID)
		}
		if needsUpdate {
			result.Changed = true
			if !request.DryRun {
				updates = append(updates, manifestedit.Update{Target: imageTarget(image), SourceRef: sourceRef, RuntimeRef: delivered.RuntimeRef})
			}
		}
		imageResult := ImageResult{
			ID: image.ID, Target: image.Target, Service: image.Service, Platform: target.Platform(),
			Tag: selection.Candidate.Tag, SourceRef: sourceRef, SourceDigest: selection.Candidate.Digest,
			DeliveryMode: image.Delivery.Mode, DeliveredRef: delivered.RuntimeRef, Copied: delivered.Copied,
			CopyResult: copyResult(delivered.CopyResult),
		}
		result.Images = append(result.Images, imageResult)
		if request.Config.Update.VersionSource.Type == config.VersionSourceImage && request.Config.Update.VersionSource.Image == image.ID {
			result.Version = selection.Version
			result.Channel = image.Channel
		}
	}
	if len(updates) > 0 {
		changes, err := applyManifest(request.Project.ManifestFile, updates)
		if err != nil {
			return Result{}, fmt.Errorf("apply manifest image updates: %w", err)
		}
		if len(changes) != len(updates) {
			return Result{}, errors.New("manifest editor returned an incomplete change set")
		}
	}
	logger.Info("Docker image update completed", "changed", result.Changed, "version", result.Version, "images", len(result.Images))
	return result, nil
}

func selectImages(images []config.Image, imageID string) ([]config.Image, error) {
	if len(images) == 0 {
		return nil, errors.New("no images are configured")
	}
	imageID = strings.TrimSpace(imageID)
	if imageID == "" {
		return append([]config.Image(nil), images...), nil
	}
	for _, image := range images {
		if image.ID == imageID {
			return []config.Image{image}, nil
		}
	}
	return nil, fmt.Errorf("image ID %q is not configured", imageID)
}

func imageTarget(image config.Image) manifestedit.Target {
	return manifestedit.Target{ID: image.ID, Kind: manifestedit.TargetKind(image.Target), Service: image.Service}
}

func imageRule(image config.Image) (versioning.Rule, registry.TagFilter, error) {
	compile := func(label, expression string) (*regexp.Regexp, error) {
		if strings.TrimSpace(expression) == "" {
			return nil, nil
		}
		compiled, err := regexp.Compile(expression)
		if err != nil {
			return nil, fmt.Errorf("compile image %q %s: %w", image.ID, label, err)
		}
		return compiled, nil
	}
	tagRegex, err := compile("tag_regex", image.TagRegex)
	if err != nil {
		return versioning.Rule{}, registry.TagFilter{}, err
	}
	excludeRegex, err := compile("exclude_regex", image.ExcludeRegex)
	if err != nil {
		return versioning.Rule{}, registry.TagFilter{}, err
	}
	versionRegex, err := compile("version_regex", image.VersionRegex)
	if err != nil {
		return versioning.Rule{}, registry.TagFilter{}, err
	}
	rule := versioning.Rule{
		Channel: versioning.Channel(image.Channel), Sort: versioning.Sort(image.Sort), TagRegex: tagRegex,
		ExcludeRegex: excludeRegex, VersionRegex: versionRegex, VersionTemplate: image.VersionTemplate,
	}
	filter := registry.TagFilter{Include: tagRegex, Exclude: excludeRegex}
	if rule.Sort == versioning.SortSemVer {
		filter.SemVerRule = &rule
	}
	if rule.Sort == versioning.SortUpdated {
		filter.UpdatedRule = &rule
	}
	return rule, filter, nil
}

func copyResult(value *appstore.CopyImageResult) *CopyResult {
	if value == nil {
		return nil
	}
	layers := make([]LayerProgress, 0, len(value.Progress.Layers))
	for _, layer := range value.Progress.Layers {
		layers = append(layers, LayerProgress{Hash: layer.Hash, Progress: layer.Progress})
	}
	return &CopyResult{
		SourceImage: value.SourceImage, Platform: value.Platform, LazyCatImage: value.LazyCatImage,
		Finished: value.Progress.Finished, Layers: layers,
	}
}
