package platformauth

import (
	"context"
	"sync"
)

type Provider struct {
	resolver  Resolver
	tokenFile func() string
	mu        sync.Mutex
	token     string
}

func NewProvider(resolver Resolver, tokenFile func() string) *Provider {
	return &Provider{resolver: resolver, tokenFile: tokenFile}
}

func (provider *Provider) Token(ctx context.Context) (string, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.token != "" {
		return provider.token, nil
	}
	path := ""
	if provider.tokenFile != nil {
		path = provider.tokenFile()
	}
	resolved, err := provider.resolver.Resolve(ctx, Request{TokenFile: path})
	if err != nil {
		return "", err
	}
	token, err := resolved.Provider.Token(ctx)
	if err != nil {
		return "", err
	}
	provider.token = token
	return token, nil
}
