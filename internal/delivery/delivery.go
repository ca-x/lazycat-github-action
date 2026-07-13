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
	Image        config.Image
	Tag          string
	SourceRef    string
	SourceDigest string
	Target       platform.Target
	DryRun       bool
	OnProgress   func(appstore.CopyProgress)
}

type Result struct {
	Mode       string                    `json:"mode"`
	RuntimeRef string                    `json:"runtimeRef"`
	Copied     bool                      `json:"copied"`
	CopyResult *appstore.CopyImageResult `json:"copyResult,omitempty"`
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
		return Result{Mode: mode, RuntimeRef: request.SourceRef}, nil
	case "mirror":
		runtimeRef, err := expandTemplate(request.Image.Delivery.ImageTemplate, request)
		if err != nil {
			return Result{}, err
		}
		result := Result{Mode: mode, RuntimeRef: runtimeRef}
		if request.DryRun || !request.Image.Delivery.RequireDigestMatch {
			return result, nil
		}
		if resolver.Inspector == nil {
			return Result{}, errors.New("mirror digest verification requires an image inspector")
		}
		var mirror registry.Image
		if inspector, ok := resolver.Inspector.(targetImageInspector); ok {
			mirror, err = inspector.InspectTarget(ctx, runtimeRef, target)
		} else {
			if target.Arch != platform.DefaultTargetArch {
				return Result{}, errors.New("configured target requires a target-aware image inspector")
			}
			mirror, err = resolver.Inspector.Inspect(ctx, runtimeRef)
		}
		if err != nil {
			return Result{}, fmt.Errorf("inspect mirror image %q: %w", runtimeRef, err)
		}
		if mirror.Platform != target.Platform() {
			return Result{}, fmt.Errorf("mirror image %q uses platform %q instead of %q", runtimeRef, mirror.Platform, target.Platform())
		}
		if !strings.EqualFold(strings.TrimSpace(mirror.Digest), strings.TrimSpace(request.SourceDigest)) {
			return Result{}, fmt.Errorf("mirror digest %q does not match source digest %q", mirror.Digest, request.SourceDigest)
		}
		return result, nil
	case "lazycat":
		result := Result{Mode: mode}
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
		return result, nil
	default:
		return Result{}, fmt.Errorf("unsupported image delivery mode %q", mode)
	}
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
