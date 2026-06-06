package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

type duckDuckGo struct {
	limit int
}

func NewDuckDuckGo(limit int) Client {
	return &duckDuckGo{limit: limit}
}

func (c *duckDuckGo) Search(ctx context.Context, query string) ([]Result, error) {
	endpoint := "https://api.duckduckgo.com/?format=json&no_html=1&skip_disambig=1&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
		AbstractText  string `json:"AbstractText"`
		AbstractURL   string `json:"AbstractURL"`
		Heading       string `json:"Heading"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]Result, 0, c.limit)
	if body.AbstractText != "" {
		out = append(out, Result{Title: body.Heading, URL: body.AbstractURL, Snippet: body.AbstractText})
	}
	for _, item := range body.RelatedTopics {
		if len(out) >= c.limit {
			break
		}
		if item.Text != "" && item.FirstURL != "" {
			out = append(out, Result{Title: item.Text, URL: item.FirstURL, Snippet: item.Text})
		}
	}
	return out, nil
}
