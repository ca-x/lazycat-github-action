package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/registry"
	"github.com/lib-x/lzc-toolkit-go/appstore"
)

type Copier interface {
	CopyImage(context.Context, appstore.CopyImageRequest) (appstore.CopyImageResult, error)
}

type ImageInspector interface {
	Inspect(context.Context, string) (registry.Image, error)
}

type targetImageInspector interface {
	InspectTarget(context.Context, string, platform.Target) (registry.Image, error)
}

type Request struct {
	Image         config.Image
	Tag           string
	SourceRef     string
	SourceDigest  string
	CurrentRef    string
	CurrentDigest string
	Mutable       bool
	Target        platform.Target
	DryRun        bool
	OnProgress    func(appstore.CopyProgress)
}

type Result struct {
	Mode            string                    `json:"mode"`
	RuntimeRef      string                    `json:"runtimeRef"`
	CurrentDigest   string                    `json:"currentDigest,omitempty"`
	DigestChanged   bool                      `json:"digestChanged,omitempty"`
	DeliveryChanged bool                      `json:"deliveryChanged,omitempty"`
	Copied          bool                      `json:"copied"`
	CopyResult      *appstore.CopyImageResult `json:"copyResult,omitempty"`
}

type Resolver struct {
	Copier    Copier
	Inspector ImageInspector
}

func (resolver Resolver) Deliver(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("delivery context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("delivery cancelled: %w", err)
	}
	if strings.TrimSpace(request.SourceRef) == "" {
		return Result{}, errors.New("source image reference is required")
	}
	target, err := request.Target.Normalize()
	if err != nil {
		return Result{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(request.Image.Delivery.Mode))
	switch mode {
	case "direct":
		if request.Mutable {
			currentDigest, err := pinnedDigest(request.CurrentRef)
			if err != nil {
				return Result{}, fmt.Errorf("inspect current direct image: %w", err)
			}
			sourceDigest, err := normalizedDigest(request.SourceDigest)
			if err != nil {
				return Result{}, err
			}
			return Result{
				Mode: mode, RuntimeRef: request.SourceRef + "@" + sourceDigest,
				CurrentDigest: currentDigest, DigestChanged: !strings.EqualFold(currentDigest, sourceDigest),
			}, nil
		}
		return Result{Mode: mode, RuntimeRef: request.SourceRef}, nil
	case "mirror":
		runtimeRef, err := expandTemplate(request.Image.Delivery.ImageTemplate, request)
		if err != nil {
			return Result{}, err
		}
		result := Result{Mode: mode, RuntimeRef: runtimeRef}
		if request.Mutable {
			if !request.Image.Delivery.RequireDigestMatch {
				return Result{}, errors.New("mutable mirror delivery requires digest verification")
			}
			if resolver.Inspector == nil {
				return Result{}, errors.New("mutable mirror delivery requires an image inspector")
			}
			currentDigest, digestErr := pinnedDigest(request.CurrentRef)
			if digestErr != nil {
				return Result{}, fmt.Errorf("inspect current mirror image: %w", digestErr)
			}
			current, inspectErr := resolver.inspect(ctx, request.CurrentRef, target)
			if inspectErr != nil {
				return Result{}, fmt.Errorf("inspect current mirror image %q: %w", request.CurrentRef, inspectErr)
			}
			if !strings.EqualFold(strings.TrimSpace(current.Digest), currentDigest) {
				return Result{}, fmt.Errorf("current mirror digest %q does not match pinned digest %q", current.Digest, currentDigest)
			}
			result.CurrentDigest = currentDigest
			sourceDigest, digestErr := normalizedDigest(request.SourceDigest)
			if digestErr != nil {
				return Result{}, digestErr
			}
			if !strings.Contains(runtimeRef, "@") {
				runtimeRef += "@" + sourceDigest
			}
			result.RuntimeRef = runtimeRef
		}
		if request.DryRun && !request.Mutable || !request.Image.Delivery.RequireDigestMatch {
			return result, nil
		}
		if resolver.Inspector == nil {
			return Result{}, errors.New("mirror digest verification requires an image inspector")
		}
		mirror, err := resolver.inspect(ctx, result.RuntimeRef, target)
		if err != nil {
			return Result{}, fmt.Errorf("inspect mirror image %q: %w", runtimeRef, err)
		}
		if mirror.Platform != target.Platform() {
			return Result{}, fmt.Errorf("mirror image %q uses platform %q instead of %q", runtimeRef, mirror.Platform, target.Platform())
		}
		if !strings.EqualFold(strings.TrimSpace(mirror.Digest), strings.TrimSpace(request.SourceDigest)) {
			return Result{}, fmt.Errorf("mirror digest %q does not match source digest %q", mirror.Digest, request.SourceDigest)
		}
		if request.Mutable {
			result.DigestChanged = !strings.EqualFold(strings.TrimSpace(result.CurrentDigest), strings.TrimSpace(request.SourceDigest))
			if !result.DigestChanged {
				result.RuntimeRef = request.CurrentRef
			}
		}
		return result, nil
	case "lazycat":
		result := Result{Mode: mode}
		if request.Mutable {
			sourceDigest, digestErr := normalizedDigest(request.SourceDigest)
			if digestErr != nil {
				return Result{}, digestErr
			}
			currentIsLazyCat := strings.HasPrefix(strings.TrimSpace(request.CurrentRef), "registry.lazycat.cloud/")
			result.RuntimeRef = request.CurrentRef
			if strings.TrimSpace(request.CurrentDigest) != "" {
				currentDigest, currentErr := normalizedDigest(request.CurrentDigest)
				if currentErr != nil {
					return Result{}, fmt.Errorf("current digest: %w", currentErr)
				}
				result.CurrentDigest = currentDigest
				result.DigestChanged = !strings.EqualFold(currentDigest, sourceDigest)
				result.DeliveryChanged = result.DigestChanged || !currentIsLazyCat
				if !result.DeliveryChanged || request.DryRun {
					return result, nil
				}
			} else if request.DryRun {
				if currentIsLazyCat {
					return Result{}, errors.New("mutable LazyCat digest baseline is missing; run a trusted non-dry migration once")
				}
				result.DeliveryChanged = true
				return result, nil
			}
		}
		if request.DryRun {
			return result, nil
		}
		if resolver.Copier == nil {
			return Result{}, errors.New("LazyCat delivery requires an authenticated image copier")
		}
		copied, err := resolver.Copier.CopyImage(ctx, appstore.CopyImageRequest{
			Image: request.SourceRef, Platform: target.Arch, OnProgress: request.OnProgress,
		})
		if err != nil {
			return Result{}, fmt.Errorf("copy image to LazyCat registry: %w", err)
		}
		if strings.TrimSpace(copied.SourceImage) != request.SourceRef || copied.Platform != target.Arch || !strings.HasPrefix(strings.TrimSpace(copied.LazyCatImage), "registry.lazycat.cloud/") || !copied.Progress.Finished {
			return Result{}, fmt.Errorf("invalid LazyCat copy result for %q", request.SourceRef)
		}
		result.RuntimeRef = copied.LazyCatImage
		result.Copied = true
		result.CopyResult = &copied
		if request.Mutable && strings.TrimSpace(request.CurrentDigest) == "" {
			currentIsLazyCat := strings.HasPrefix(strings.TrimSpace(request.CurrentRef), "registry.lazycat.cloud/")
			result.DeliveryChanged = true
			result.DigestChanged = currentIsLazyCat && strings.TrimSpace(copied.LazyCatImage) != strings.TrimSpace(request.CurrentRef)
		}
		return result, nil
	default:
		return Result{}, fmt.Errorf("unsupported image delivery mode %q", mode)
	}
}

func (resolver Resolver) inspect(ctx context.Context, reference string, target platform.Target) (registry.Image, error) {
	if strings.TrimSpace(reference) == "" {
		return registry.Image{}, errors.New("current image reference is required")
	}
	if inspector, ok := resolver.Inspector.(targetImageInspector); ok {
		return inspector.InspectTarget(ctx, reference, target)
	}
	if target.Arch != platform.DefaultTargetArch {
		return registry.Image{}, errors.New("configured target requires a target-aware image inspector")
	}
	return resolver.Inspector.Inspect(ctx, reference)
}

func normalizedDigest(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return "", fmt.Errorf("source digest %q must be a sha256 digest", value)
	}
	for _, character := range strings.TrimPrefix(value, "sha256:") {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return "", fmt.Errorf("source digest %q must be a sha256 digest", value)
		}
	}
	return value, nil
}

func pinnedDigest(reference string) (string, error) {
	_, digest, found := strings.Cut(strings.TrimSpace(reference), "@")
	if !found {
		return "", fmt.Errorf("current image reference %q is not digest-pinned", reference)
	}
	return normalizedDigest(digest)
}

func expandTemplate(template string, request Request) (string, error) {
	value := strings.TrimSpace(template)
	if value == "" {
		return "", errors.New("mirror image template is required")
	}
	value = strings.ReplaceAll(value, "{tag}", request.Tag)
	value = strings.ReplaceAll(value, "{digest}", request.SourceDigest)
	value = strings.ReplaceAll(value, "{source}", request.SourceRef)
	if strings.Contains(value, "{") || strings.Contains(value, "}") {
		return "", fmt.Errorf("mirror image template contains an unsupported placeholder: %q", template)
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.New("mirror image template produced an empty reference")
	}
	return value, nil
}
