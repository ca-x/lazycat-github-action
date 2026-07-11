package storelookup

import (
	"context"
	"errors"
	"net/http"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	officialstore "github.com/lib-x/lzc-toolkit-go/appstore/official"
	privatestore "github.com/lib-x/lzc-toolkit-go/appstore/private"
)

type Store string

const (
	StoreOfficial Store = "official"
	StorePrivate  Store = "private"
)

type Request struct {
	Store      Store
	PackageID  string
	BaseURL    string
	GroupCodes []string
	HTTPClient *http.Client
}

type Result struct {
	OnlineVersion string
}

type Lookup func(context.Context, Request) (Result, error)

func Default(ctx context.Context, request Request) (Result, error) {
	var version string
	switch request.Store {
	case StoreOfficial:
		client := officialstore.New(officialstore.Options{
			MetadataBaseURL: strings.TrimSpace(request.BaseURL),
			HTTPClient:      request.HTTPClient,
		})
		application, err := client.Application(ctx, request.PackageID)
		if err != nil {
			return Result{}, err
		}
		version = application.Version.Name
	case StorePrivate:
		client, err := privatestore.New(privatestore.Options{
			BaseURL:    strings.TrimSpace(request.BaseURL),
			HTTPClient: request.HTTPClient,
			GroupCodes: append([]string(nil), request.GroupCodes...),
		})
		if err != nil {
			return Result{}, err
		}
		latest, err := client.LatestVersion(ctx, privatestore.LatestVersionRequest{PackageID: request.PackageID})
		if err != nil {
			return Result{}, err
		}
		version = latest.LatestVersion.Version
	default:
		return Result{}, &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "storelookup", Cause: errors.New("unsupported store")}
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return Result{}, &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, Op: "storelookup", Cause: errors.New("store returned an empty latest version")}
	}
	return Result{OnlineVersion: version}, nil
}
