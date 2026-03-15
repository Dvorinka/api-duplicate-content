package similarity

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Service struct {
	client      *http.Client
	maxTextSize int
}

type CompareInput struct {
	TextA string `json:"text_a"`
	TextB string `json:"text_b"`
}

type CompareURLInput struct {
	URLA string `json:"url_a"`
	URLB string `json:"url_b"`
}

type CompareResult struct {
	SimilarityScore    float64 `json:"similarity_score"`
	PlagiarismPercent  float64 `json:"plagiarism_percent"`
	TokenCountA        int     `json:"token_count_a"`
	TokenCountB        int     `json:"token_count_b"`
	SharedShingleCount int     `json:"shared_shingle_count"`
	ShingleCountA      int     `json:"shingle_count_a"`
	ShingleCountB      int     `json:"shingle_count_b"`
	Assessment         string  `json:"assessment"`
}

type CompareURLResult struct {
	CompareResult
	URLA string `json:"url_a"`
	URLB string `json:"url_b"`
}

func NewService(timeout time.Duration, maxTextSize int) *Service {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	if maxTextSize <= 0 {
		maxTextSize = 200_000
	}
	return &Service{
		client:      &http.Client{Timeout: timeout},
		maxTextSize: maxTextSize,
	}
}

func (s *Service) CompareText(input CompareInput) (CompareResult, error) {
	textA := strings.TrimSpace(input.TextA)
	textB := strings.TrimSpace(input.TextB)
	if textA == "" || textB == "" {
		return CompareResult{}, errors.New("text_a and text_b are required")
	}
	if len(textA) > s.maxTextSize || len(textB) > s.maxTextSize {
		return CompareResult{}, errors.New("text input exceeds maximum size")
	}
	return compare(textA, textB), nil
}

func (s *Service) CompareURLs(ctx context.Context, input CompareURLInput) (CompareURLResult, error) {
	urlA, err := normalizeURL(input.URLA)
	if err != nil {
		return CompareURLResult{}, err
	}
	urlB, err := normalizeURL(input.URLB)
	if err != nil {
		return CompareURLResult{}, err
	}

	textA, err := s.fetchURLText(ctx, urlA)
	if err != nil {
		return CompareURLResult{}, err
	}
	textB, err := s.fetchURLText(ctx, urlB)
	if err != nil {
		return CompareURLResult{}, err
	}

	result := compare(textA, textB)
	return CompareURLResult{
		CompareResult: result,
		URLA:          urlA,
		URLB:          urlB,
	}, nil
}

func (s *Service) fetchURLText(ctx context.Context, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "apiservices-duplicate-content/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", errors.New("failed to fetch url content")
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(s.maxTextSize)+1))
	if err != nil {
		return "", err
	}
	if len(data) > s.maxTextSize {
		return "", errors.New("url content exceeds maximum size")
	}
	return extractVisibleText(string(data)), nil
}

func compare(textA, textB string) CompareResult {
	tokensA := tokenize(textA)
	tokensB := tokenize(textB)
	shinglesA := buildShingles(tokensA, 3)
	shinglesB := buildShingles(tokensB, 3)

	shared := 0
	for shingle := range shinglesA {
		if _, ok := shinglesB[shingle]; ok {
			shared++
		}
	}

	union := len(shinglesA) + len(shinglesB) - shared
	score := 0.0
	if union > 0 {
		score = float64(shared) / float64(union)
	}
	percent := round2(score * 100)

	return CompareResult{
		SimilarityScore:    round4(score),
		PlagiarismPercent:  percent,
		TokenCountA:        len(tokensA),
		TokenCountB:        len(tokensB),
		SharedShingleCount: shared,
		ShingleCountA:      len(shinglesA),
		ShingleCountB:      len(shinglesB),
		Assessment:         assessment(score),
	}
}

var nonWordRe = regexp.MustCompile(`[^a-z0-9]+`)

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	lower = nonWordRe.ReplaceAllString(lower, " ")
	parts := strings.Fields(lower)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}
		out = append(out, part)
	}
	return out
}

func buildShingles(tokens []string, n int) map[string]struct{} {
	out := make(map[string]struct{})
	if len(tokens) == 0 {
		return out
	}
	if len(tokens) < n {
		out[strings.Join(tokens, " ")] = struct{}{}
		return out
	}
	for i := 0; i <= len(tokens)-n; i++ {
		shingle := strings.Join(tokens[i:i+n], " ")
		out[shingle] = struct{}{}
	}
	return out
}

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("url scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", errors.New("invalid url host")
	}
	return parsed.String(), nil
}

func extractVisibleText(htmlBody string) string {
	doc, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		return htmlBody
	}

	var b strings.Builder
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, skip bool) {
		if node == nil {
			return
		}

		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			if tag == "script" || tag == "style" || tag == "noscript" {
				skip = true
			}
		}

		if node.Type == html.TextNode && !skip {
			text := strings.TrimSpace(node.Data)
			if text != "" {
				b.WriteString(text)
				b.WriteByte('\n')
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skip)
		}
	}
	walk(doc, false)
	return b.String()
}

func assessment(score float64) string {
	switch {
	case score >= 0.8:
		return "very_high_overlap"
	case score >= 0.6:
		return "high_overlap"
	case score >= 0.3:
		return "moderate_overlap"
	default:
		return "low_overlap"
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
