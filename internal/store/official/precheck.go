package official

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/lint"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

func officialPrecheck(ctx context.Context, lpkPath string) error {
	reader, err := lpk.OpenFile(ctx, lpkPath)
	if err != nil {
		return officialPrecheckError(ctx, err)
	}
	extractionParent, err := os.MkdirTemp("", "lazycat-action-official-lint-*")
	if err != nil {
		return officialPrecheckError(ctx, errors.Join(err, reader.Close()))
	}
	defer os.RemoveAll(extractionParent)

	extractionRoot := filepath.Join(extractionParent, "root")
	extractErr := reader.Extract(ctx, extractionRoot)
	closeErr := reader.Close()
	if extractErr != nil || closeErr != nil {
		return officialPrecheckError(ctx, errors.Join(extractErr, closeErr))
	}
	warnings, err := lint.Package(ctx, os.DirFS(extractionRoot), lint.WithOfficial())
	if err != nil {
		return officialPrecheckError(ctx, err)
	}
	for _, warning := range warnings {
		if lint.IsOfficialWarning(warning) {
			return publishError(lpkgo.CodeInvalidManifest, errors.New("official manifest validation failed"))
		}
	}
	return nil
}

func officialPrecheckError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return publishContextError(ctxErr)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return publishContextError(err)
	}
	code := lpkgo.CodeCommandFailed
	var toolkitError *lpkgo.Error
	if errors.As(err, &toolkitError) && toolkitError.Code != "" {
		code = toolkitError.Code
	}
	return &lpkgo.Error{
		Code:  code,
		Op:    "store.official.precheck",
		Cause: errors.New("official manifest precheck failed"),
	}
}
