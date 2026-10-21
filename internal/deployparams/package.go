package deployparams

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/lib-x/lzc-toolkit-go/archive"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

// Package validates deployment metadata in an LPK without extracting payloads.
// Packages without installation parameters remain valid.
func Package(ctx context.Context, reader *lpk.Reader) error {
	entries, err := reader.Entries(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name != "deploy_params.yml" {
			continue
		}
		v := validator{source: entry.Name}
		if entry.Type != archive.EntryRegular {
			return v.fail(nil, "$", "deployment parameters must be a regular file")
		}
		if entry.Size > MaxDocumentBytes {
			return v.fail(nil, "$", "deployment parameter document exceeds 8 MiB")
		}
		file, err := reader.OpenEntry(ctx, entry.Name)
		if err != nil {
			return fmt.Errorf("open deployment parameters: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
		if err := errors.Join(readErr, file.Close()); err != nil {
			return fmt.Errorf("read deployment parameters: %w", err)
		}
		return Validate(data, entry.Name)
	}
	return nil
}
