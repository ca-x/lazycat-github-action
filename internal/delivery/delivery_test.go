package delivery_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/delivery"
	"github.com/ca-x/lazycat-github-action/internal/registry"
	"github.com/lib-x/lzc-toolkit-go/appstore"
)

func TestLazyCatDeliveryCopiesAMD64AndReturnsCopyResult(t *testing.T) {
	copier := &fakeCopier{result: appstore.CopyImageResult{
		SourceImage:  "ghcr.io/acme/web:v1.2.3",
		Platform:     "amd64",
		LazyCatImage: "registry.lazycat.cloud/acme/web:v1.2.3",
		Progress:     appstore.CopyProgress{Finished: true, Layers: []appstore.LayerProgress{{Hash: "sha256:layer", Progress: 100}}},
	}}
	resolver := delivery.Resolver{Copier: copier}
	result, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}},
		Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if copier.request.Platform != "amd64" || copier.request.Image != "ghcr.io/acme/web:v1.2.3" {
		t.Fatalf("request=%#v", copier.request)
	}
	if result.RuntimeRef != "registry.lazycat.cloud/acme/web:v1.2.3" || !result.Copied || result.CopyResult == nil || !result.CopyResult.Progress.Finished {
		t.Fatalf("result=%#v", result)
	}
}

func TestDirectDeliveryUsesSourceWithoutExternalCalls(t *testing.T) {
	copier := &fakeCopier{err: errors.New("must not copy")}
	inspector := &fakeInspector{err: errors.New("must not inspect")}
	resolver := delivery.Resolver{Copier: copier, Inspector: inspector}
	result, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "direct"}},
		Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeRef != "ghcr.io/acme/web:v1.2.3" || result.Copied || copier.calls != 0 || inspector.calls != 0 {
		t.Fatalf("result=%#v copier=%d inspector=%d", result, copier.calls, inspector.calls)
	}
}

func TestMirrorDeliveryExpandsTemplateAndVerifiesDigest(t *testing.T) {
	inspector := &fakeInspector{result: registry.Image{Digest: digest("a"), Platform: "linux/amd64"}}
	resolver := delivery.Resolver{Inspector: inspector}
	result, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "ghcr.1ms.run/acme/web:{tag}", RequireDigestMatch: true}},
		Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeRef != "ghcr.1ms.run/acme/web:v1.2.3" || inspector.reference != result.RuntimeRef || result.Copied {
		t.Fatalf("result=%#v inspector=%#v", result, inspector)
	}
}

func TestMirrorDeliveryRejectsDigestMismatch(t *testing.T) {
	resolver := delivery.Resolver{Inspector: &fakeInspector{result: registry.Image{Digest: digest("b"), Platform: "linux/amd64"}}}
	_, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "mirror/acme/web:{tag}", RequireDigestMatch: true}},
		Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
	})
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("err=%v", err)
	}
}

func TestDryRunDoesNotCopyOrInspect(t *testing.T) {
	resolver := delivery.Resolver{Copier: &fakeCopier{err: errors.New("must not copy")}, Inspector: &fakeInspector{err: errors.New("must not inspect")}}
	for _, image := range []config.Image{
		{ID: "lazycat", Delivery: config.Delivery{Mode: "lazycat"}},
		{ID: "mirror", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "mirror/acme/web:{tag}", RequireDigestMatch: true}},
	} {
		result, err := resolver.Deliver(context.Background(), delivery.Request{Image: image, Tag: "v1", SourceRef: "source:v1", SourceDigest: digest("a"), DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if result.Copied {
			t.Fatalf("result=%#v", result)
		}
	}
}

type fakeCopier struct {
	request appstore.CopyImageRequest
	result  appstore.CopyImageResult
	err     error
	calls   int
}

func (copier *fakeCopier) CopyImage(_ context.Context, request appstore.CopyImageRequest) (appstore.CopyImageResult, error) {
	copier.calls++
	copier.request = request
	return copier.result, copier.err
}

type fakeInspector struct {
	reference string
	result    registry.Image
	err       error
	calls     int
}

func (inspector *fakeInspector) Inspect(_ context.Context, reference string) (registry.Image, error) {
	inspector.calls++
	inspector.reference = reference
	return inspector.result, inspector.err
}

func digest(character string) string { return "sha256:" + strings.Repeat(character, 64) }
