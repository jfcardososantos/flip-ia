package services

import (
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"net/url"
	"strings"

	xhtml "golang.org/x/net/html"
)

const duckDuckGoSearchEndpoint = "https://html.duckduckgo.com/html/"

type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchWeb provides a small, keyless search tool for the dashboard chat. It
// only contacts the fixed DuckDuckGo HTML endpoint and returns compact results
// that can safely be fed back to a model during a tool-calling loop.
func SearchWeb(ctx context.Context, query string, limit int) ([]WebSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("a consulta de busca está vazia")
	}
	if limit < 1 {
		limit = 5
	}
	if limit > 8 {
		limit = 8
	}

	searchURL := duckDuckGoSearchEndpoint + "?" + url.Values{"q": {query}, "kl": {"br-pt"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en;q=0.7")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; flip-ai/1.0; +https://ia.alfst.com.br)")

	resp, err := GlobalHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("o buscador retornou HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	document, err := xhtml.Parse(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("falha ao interpretar os resultados: %w", err)
	}
	results := parseDuckDuckGoResults(document, limit)
	if len(results) == 0 {
		return nil, errors.New("nenhum resultado encontrado")
	}
	return results, nil
}

func parseDuckDuckGoResults(root *xhtml.Node, limit int) []WebSearchResult {
	results := make([]WebSearchResult, 0, limit)
	seen := make(map[string]bool)
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if len(results) >= limit {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "div" && nodeHasClass(node, "result") {
			link := findDescendantWithClass(node, "a", "result__a")
			if link != nil {
				target := cleanSearchResultURL(nodeAttribute(link, "href"))
				title := compactNodeText(link)
				if target != "" && title != "" && !seen[target] {
					snippetNode := findDescendantWithClass(node, "", "result__snippet")
					results = append(results, WebSearchResult{
						Title:   title,
						URL:     target,
						Snippet: compactNodeText(snippetNode),
					})
					seen[target] = true
				}
			}
		}
		for child := node.FirstChild; child != nil && len(results) < limit; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return results
}

func nodeHasClass(node *xhtml.Node, expected string) bool {
	for _, attr := range node.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, className := range strings.Fields(attr.Val) {
			if className == expected {
				return true
			}
		}
	}
	return false
}

func findDescendantWithClass(node *xhtml.Node, tag, className string) *xhtml.Node {
	if node == nil {
		return nil
	}
	if node.Type == xhtml.ElementNode && (tag == "" || node.Data == tag) && nodeHasClass(node, className) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findDescendantWithClass(child, tag, className); found != nil {
			return found
		}
	}
	return nil
}

func nodeAttribute(node *xhtml.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func compactNodeText(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	var parts []string
	var walk func(*xhtml.Node)
	walk = func(current *xhtml.Node) {
		if current.Type == xhtml.TextNode {
			if text := strings.TrimSpace(stdhtml.UnescapeString(current.Data)); text != "" {
				parts = append(parts, text)
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func cleanSearchResultURL(raw string) string {
	raw = stdhtml.UnescapeString(strings.TrimSpace(raw))
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if target := parsed.Query().Get("uddg"); target != "" {
		raw = target
		parsed, err = url.Parse(raw)
		if err != nil {
			return ""
		}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}
