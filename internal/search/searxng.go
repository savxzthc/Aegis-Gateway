package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

type searxNG struct {
	baseURL string
	limit   int
}

func NewSearxNG(baseURL string, limit int) Client {
	return &searxNG{baseURL: baseURL, limit: limit}
}

func (c *searxNG) Search(ctx context.Context, query string) ([]Result, error) {
	endpoint, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return nil, err
	}
	values := endpoint.Query()
	values.Set("q", query)
	values.Set("format", "json")
	endpoint.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil
	}
	var body struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]Result, 0, min(c.limit, len(body.Results)))
	for _, item := range body.Results {
		if len(out) >= c.limit {
			break
		}
		out = append(out, Result{Title: item.Title, URL: item.URL, Snippet: item.Content})
	}
	return out, nil
}
