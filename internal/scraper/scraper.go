package scraper

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"

	"github.com/demirbey05/erdos-agent/internal/models"
)

const problemsYAMLURL = "https://raw.githubusercontent.com/teorth/erdosproblems/main/data/problems.yaml"
const problemPageBaseURL = "https://www.erdosproblems.com"

var httpClient = &http.Client{Timeout: 30 * time.Second}

// FetchProblems downloads and parses the problems.yaml from the GitHub repository.
func FetchProblems() ([]models.Problem, error) {
	resp, err := httpClient.Get(problemsYAMLURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch problems YAML: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching problems YAML", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var problems []models.Problem
	if err := yaml.Unmarshal(body, &problems); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return problems, nil
}

// FetchProblemDescription scrapes the problem description text from erdosproblems.com/{number}.
func FetchProblemDescription(number string) (string, error) {
	url := fmt.Sprintf("%s/%s", problemPageBaseURL, number)
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch problem page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d for problem %s", resp.StatusCode, number)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// The problem description on erdosproblems.com is inside the main content area.
	// We look for common content containers and extract text.
	text := extractProblemText(doc)
	if text == "" {
		return "", fmt.Errorf("could not extract problem description for problem %s", number)
	}

	return text, nil
}

// extractProblemText walks the HTML tree to find and extract the problem statement.
// The site uses a structure where the problem description is in the main body area.
func extractProblemText(n *html.Node) string {
	// Strategy: find the main content div and extract all meaningful text.
	// The erdosproblems.com site renders problem text in a div after the problem header.
	var content strings.Builder
	var inMainContent bool

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Look for the problem-content area — the site uses various div structures
			for _, attr := range n.Attr {
				if attr.Key == "class" && (strings.Contains(attr.Val, "problem-statement") ||
					strings.Contains(attr.Val, "problem-content") ||
					strings.Contains(attr.Val, "main-content") ||
					strings.Contains(attr.Val, "content-body")) {
					inMainContent = true
				}
			}

			// Skip nav, header, footer, and script elements
			if n.Data == "nav" || n.Data == "header" || n.Data == "footer" ||
				n.Data == "script" || n.Data == "style" || n.Data == "noscript" {
				return
			}
		}

		if n.Type == html.TextNode && n.Parent != nil {
			parentTag := n.Parent.Data
			// Skip text from navigation/metadata elements
			if parentTag != "script" && parentTag != "style" && parentTag != "nav" {
				text := strings.TrimSpace(n.Data)
				if text != "" {
					content.WriteString(text)
					content.WriteString(" ")
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if inMainContent && n.Type == html.ElementNode {
			// Add paragraph breaks
			if n.Data == "p" || n.Data == "div" || n.Data == "br" {
				content.WriteString("\n")
			}
		}
	}

	walk(n)
	return cleanText(content.String())
}

// cleanText removes excessive whitespace and normalizes the extracted text.
func cleanText(s string) string {
	// Collapse multiple spaces
	lines := strings.Split(s, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

// FilterOpenProblems returns only problems with status "open".
func FilterOpenProblems(problems []models.Problem) []models.Problem {
	var open []models.Problem
	for _, p := range problems {
		if p.IsOpen() {
			open = append(open, p)
		}
	}
	return open
}

// FilterPrizeProblems returns only problems that have a monetary prize.
func FilterPrizeProblems(problems []models.Problem) []models.Problem {
	var prized []models.Problem
	for _, p := range problems {
		if p.HasPrize() {
			prized = append(prized, p)
		}
	}
	return prized
}
