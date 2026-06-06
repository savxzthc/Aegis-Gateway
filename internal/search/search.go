package search

import (
	"context"
	"fmt"

	"github.com/savxzthc/aegis-gateway/internal/config"
)

type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Client interface {
	Search(ctx context.Context, query string) ([]Result, error)
}

func NewClient(cfg config.SearchConfig) (Client, error) {
	switch cfg.Provider {
	case "searxng":
		return NewSearxNG(cfg.SearxNGBaseURL, cfg.MaxResults), nil
	case "duckduckgo":
		return NewDuckDuckGo(cfg.MaxResults), nil
	default:
		return nil, fmt.Errorf("unsupported search provider %q", cfg.Provider)
	}
}
