package imagebuild

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/ca-x/lazycat-github-action/internal/platform"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func validateImage(ctx context.Context, image v1.Image, target platform.Target) error {
	config, err := image.ConfigFile()
	if err != nil {
		return err
	}
	if config.OS != target.OS || config.Architecture != target.Arch {
		return fmt.Errorf("image platform %s/%s does not match requested %s", config.OS, config.Architecture, target.Platform())
	}
	manifest, err := image.Manifest()
	if err != nil {
		return err
	}
	if manifest.Config.MediaType != types.OCIConfigJSON {
		return errors.New("expected OCI image config")
	}
	layers, err := image.Layers()
	if err != nil {
		return err
	}
	if config.RootFS.Type != "layers" {
		return errors.New("image rootfs must have type layers")
	}
	if len(layers) != len(config.RootFS.DiffIDs) {
		return errors.New("image layer count does not match rootfs diff IDs")
	}
	for index, layer := range layers {
		mediaType, err := layer.MediaType()
		if err != nil {
			return err
		}
		if mediaType != types.OCILayer && mediaType != types.OCIUncompressedLayer {
			return errors.New("only gzip and uncompressed OCI layers are supported")
		}
		stream, err := layer.Uncompressed()
		if err != nil {
			return err
		}
		hash := sha256.New()
		reader := io.TeeReader(contextReader{ctx: ctx, reader: stream}, hash)
		scanErr := scanTar(reader)
		closeErr := stream.Close()
		if err := errors.Join(scanErr, closeErr); err != nil {
			return fmt.Errorf("layer %d is incomplete or invalid: %w", index, err)
		}
		if fmt.Sprintf("sha256:%x", hash.Sum(nil)) != config.RootFS.DiffIDs[index].String() {
			return fmt.Errorf("layer %d rootfs diff ID mismatch", index)
		}
	}
	return nil
}

// Drain every entry and the underlying stream, including gzip CRC and trailer.
func scanTar(reader io.Reader) error {
	archive := tar.NewReader(reader)
	for {
		_, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, archive); err != nil {
			return err
		}
	}
	_, err := io.Copy(io.Discard, reader)
	return err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
