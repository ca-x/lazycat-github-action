package registry

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/versioning"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const maxTags = 10000

var ErrPlatformNotFound = errors.New("linux/amd64 image platform not found")

var targetPlatform = v1.Platform{OS: platform.TargetOS, Architecture: platform.TargetArch}

type Image struct {
	Reference string    `json:"reference"`
	Digest    string    `json:"digest"`
	Created   time.Time `json:"created"`
	Platform  string    `json:"platform"`
}

type Client struct {
	options []remote.Option
}

type TagFilter struct {
	Include *regexp.Regexp
	Exclude *regexp.Regexp
}

func New(options ...remote.Option) *Client {
	return &Client{options: append([]remote.Option(nil), options...)}
}

func (client *Client) Candidates(ctx context.Context, source string, filters ...TagFilter) ([]versioning.Candidate, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	repository, err := name.NewRepository(strings.TrimSpace(source), name.WeakValidation)
	if err != nil {
		return nil, fmt.Errorf("parse image repository %q: %w", source, err)
	}
	tags, err := remote.List(repository, client.remoteOptions(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("list image tags for %q: %w", source, err)
	}
	if len(tags) > maxTags {
		return nil, fmt.Errorf("image repository %q returned %d tags; limit is %d", source, len(tags), maxTags)
	}
	sort.Strings(tags)
	candidates := make([]versioning.Candidate, 0, len(tags))
	missingPlatform := 0
	var filter TagFilter
	if len(filters) > 0 {
		filter = filters[0]
	}
	for _, tag := range tags {
		if filter.Include != nil && !filter.Include.MatchString(tag) {
			continue
		}
		if filter.Exclude != nil && filter.Exclude.MatchString(tag) {
			continue
		}
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		image, err := client.Inspect(ctx, repository.Tag(tag).Name())
		if err != nil {
			if errors.Is(err, ErrPlatformNotFound) {
				missingPlatform++
				continue
			}
			return nil, fmt.Errorf("inspect image tag %q: %w", tag, err)
		}
		candidates = append(candidates, versioning.Candidate{Tag: tag, Digest: image.Digest, Created: image.Created})
	}
	if len(candidates) == 0 && missingPlatform > 0 {
		return nil, fmt.Errorf("%w for repository %q", ErrPlatformNotFound, source)
	}
	return candidates, nil
}

func (client *Client) Inspect(ctx context.Context, reference string) (Image, error) {
	if err := checkContext(ctx); err != nil {
		return Image{}, err
	}
	parsed, err := name.ParseReference(strings.TrimSpace(reference), name.WeakValidation)
	if err != nil {
		return Image{}, fmt.Errorf("parse image reference %q: %w", reference, err)
	}
	descriptor, err := remote.Get(parsed, client.remoteOptions(ctx)...)
	if err != nil {
		return Image{}, fmt.Errorf("get image manifest %q: %w", reference, err)
	}
	if descriptor.MediaType.IsIndex() {
		index, err := descriptor.ImageIndex()
		if err != nil {
			return Image{}, fmt.Errorf("read image index %q: %w", reference, err)
		}
		manifest, err := index.IndexManifest()
		if err != nil {
			return Image{}, fmt.Errorf("read image index manifest %q: %w", reference, err)
		}
		matched := false
		for _, child := range manifest.Manifests {
			candidate := v1.Platform{OS: platform.TargetOS, Architecture: platform.TargetArch}
			if child.Platform != nil {
				candidate = *child.Platform
			}
			if candidate.Satisfies(targetPlatform) {
				matched = true
				break
			}
		}
		if !matched {
			return Image{}, fmt.Errorf("%w: image index %q", ErrPlatformNotFound, reference)
		}
	}
	image, err := descriptor.Image()
	if err != nil {
		return Image{}, fmt.Errorf("resolve %s image for %q: %w", platform.TargetPlatform, reference, err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		return Image{}, fmt.Errorf("read image config for %q: %w", reference, err)
	}
	if config.OS != platform.TargetOS || config.Architecture != platform.TargetArch {
		return Image{}, fmt.Errorf("%w: image %q resolved to %s/%s", ErrPlatformNotFound, reference, config.OS, config.Architecture)
	}
	digest, err := image.Digest()
	if err != nil {
		return Image{}, fmt.Errorf("read image digest for %q: %w", reference, err)
	}
	return Image{
		Reference: parsed.Name(),
		Digest:    digest.String(),
		Created:   config.Created.Time.UTC(),
		Platform:  platform.TargetPlatform,
	}, nil
}

func (client *Client) remoteOptions(ctx context.Context) []remote.Option {
	options := []remote.Option{remote.WithAuthFromKeychain(authn.DefaultKeychain)}
	if client != nil {
		options = append(options, client.options...)
	}
	return append(options, remote.WithContext(ctx), remote.WithPlatform(targetPlatform))
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("registry request cancelled: %w", err)
	}
	return nil
}
