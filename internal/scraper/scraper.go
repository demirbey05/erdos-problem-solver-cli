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

// extractProblemText walks the HTML tree to find and extract the problem statement
// and additional remarks from erdosproblems.com.
//
// The page structure has:
//   - div#content: the main problem statement (LaTeX/text)
//   - div.problem-additional-text: additional remarks and context
//
// We extract text from both, skipping navigation, footers, scripts, etc.
func extractProblemText(n *html.Node) string {
	var statement, remarks strings.Builder

	// Find and extract the content div (main problem statement)
	if contentDiv := findElementByID(n, "content"); contentDiv != nil {
		extractText(contentDiv, &statement)
	}

	// Find and extract all problem-additional-text divs (remarks)
	additionalDivs := findElementsByClass(n, "problem-additional-text")
	for _, div := range additionalDivs {
		extractText(div, &remarks)
	}

	// Combine statement and remarks
	var result strings.Builder
	stmt := cleanText(statement.String())
	rmk := cleanText(remarks.String())

	if stmt != "" {
		result.WriteString(stmt)
	}
	if rmk != "" {
		if stmt != "" {
			result.WriteString("\n\n")
		}
		result.WriteString(rmk)
	}

	return result.String()
}

// findElementByID searches the HTML tree for an element with the given id attribute.
func findElementByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == id {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElementByID(c, id); found != nil {
			return found
		}
	}
	return nil
}

// findElementsByClass searches the HTML tree for all elements with a class
// that contains the given class name.
func findElementsByClass(n *html.Node, class string) []*html.Node {
	var results []*html.Node
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			if attr.Key == "class" && containsClass(attr.Val, class) {
				results = append(results, n)
				return results // don't descend into matched elements to avoid duplicates
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		results = append(results, findElementsByClass(c, class)...)
	}
	return results
}

// containsClass checks whether a space-separated class attribute value
// contains the given class name.
func containsClass(attrVal, class string) bool {
	for _, c := range strings.Fields(attrVal) {
		if c == class {
			return true
		}
	}
	return false
}

// extractText recursively extracts visible text from an HTML node subtree,
// skipping script, style, nav, and other non-content elements.
func extractText(n *html.Node, buf *strings.Builder) {
	if n.Type == html.ElementNode {
		// Skip elements that contain no useful problem text
		switch n.Data {
		case "script", "style", "nav", "noscript", "button", "input", "form", "svg":
			return
		}

		// Skip elements with classes that are UI widgets, not problem text
		for _, attr := range n.Attr {
			if attr.Key == "class" {
				cls := attr.Val
				if strings.Contains(cls, "problem-status-widget") ||
					strings.Contains(cls, "problem-status-") ||
					strings.Contains(cls, "problem-reactions") ||
					strings.Contains(cls, "comment-count") ||
					strings.Contains(cls, "external") ||
					strings.Contains(cls, "image-container") {
					return
				}
			}
			// Skip footer-like paragraphs (LaTeX source, edit history, nav links)
			// These are styled with monospace font
			if attr.Key == "style" && strings.Contains(attr.Val, "Courier New") {
				return
			}
		}
	}

	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			buf.WriteString(text)
			buf.WriteString(" ")
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractText(c, buf)
	}

	// Add line breaks after block elements
	if n.Type == html.ElementNode {
		switch n.Data {
		case "p", "div", "br", "li":
			buf.WriteString("\n")
		}
	}
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
