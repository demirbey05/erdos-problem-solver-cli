package solver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
	anthropic "github.com/mozilla-ai/any-llm-go/providers/anthropic"
	gemini "github.com/mozilla-ai/any-llm-go/providers/gemini"
	groq "github.com/mozilla-ai/any-llm-go/providers/groq"
	openai "github.com/mozilla-ai/any-llm-go/providers/openai"

	"github.com/demirbey05/erdos-agent/internal/models"
)

// Solver sends problems to an LLM and saves the generated proofs.
type Solver struct {
	llm      anyllm.Provider
	model    string
	solnsDir string
}

// New creates a Solver backed by the given provider/model/key.
func New(provider, model, apiKey, solnsDir string) (*Solver, error) {
	var llm anyllm.Provider
	var err error

	switch provider {
	case "openai":
		llm, err = openai.New(anyllm.WithAPIKey(apiKey))
		if err != nil {
			return nil, fmt.Errorf("failed to create LLM client: %w", err)
		}

	case "groq":
		llm, err = groq.New(anyllm.WithAPIKey(apiKey))
		if err != nil {
			return nil, fmt.Errorf("failed to create LLM client: %w", err)
		}
	case "anthropic":
		llm, err = anthropic.New(anyllm.WithAPIKey(apiKey))
		if err != nil {
			return nil, fmt.Errorf("failed to create LLM client: %w", err)
		}
	case "gemini":
		llm, err = gemini.New(anyllm.WithAPIKey(apiKey))
		if err != nil {
			return nil, fmt.Errorf("failed to create LLM client: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	if err := os.MkdirAll(solnsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create solutions directory: %w", err)
	}

	return &Solver{llm: llm, solnsDir: solnsDir, model: model}, nil
}

// promptTemplate is the system prompt instructing the LLM how to approach the problem.
const promptTemplate = `Don't search the internet. This is a test to see how well you can craft non-trivial, novel and creative proofs given a "number theory and primitive sets" math problem. Provide a full unconditional proof or disproof of the problem.

%s

REMEMBER - this unconditional argument may require non-trivial, creative and novel elements.`

// Solve sends the problem to the LLM and returns the generated solution text.
func (s *Solver) Solve(ctx context.Context, problem models.Problem, description string) (string, error) {
	// Build full problem context
	var problemText strings.Builder
	problemText.WriteString(fmt.Sprintf("Erdős Problem #%s\n", problem.Number))
	if problem.HasPrize() {
		problemText.WriteString(fmt.Sprintf("Prize: %s\n", problem.Prize))
	}
	if len(problem.Tags) > 0 {
		problemText.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(problem.Tags, ", ")))
	}
	problemText.WriteString(fmt.Sprintf("\n%s", description))

	fullPrompt := fmt.Sprintf(promptTemplate, problemText.String())

	response, err := s.llm.Completion(ctx, anyllm.CompletionParams{
		Model: s.model,
		Messages: []anyllm.Message{
			{Role: anyllm.RoleUser, Content: fullPrompt},
		},
	})

	if err != nil {
		return "", fmt.Errorf("LLM generation failed for problem %s: %w", problem.Number, err)
	}
	resp := response.Choices[0].Message.Content
	respString, ok := resp.(string)
	if ok {
		return respString, nil
	}
	return respString, nil
}

// SaveSolution writes the solution to solns/{problem_id}-{attempt_id}.md.
// It auto-increments the attempt ID based on existing files.
func (s *Solver) SaveSolution(problem models.Problem, solution string) (string, error) {
	attemptID := s.nextAttemptID(problem.Number)
	filename := fmt.Sprintf("%s-%d.md", problem.Number, attemptID)
	path := filepath.Join(s.solnsDir, filename)

	// Build the solution document
	var doc strings.Builder
	doc.WriteString(fmt.Sprintf("# Erdős Problem #%s — Attempt %d\n\n", problem.Number, attemptID))
	doc.WriteString(fmt.Sprintf("**Generated:** %s\n\n", time.Now().Format(time.RFC3339)))
	if problem.HasPrize() {
		doc.WriteString(fmt.Sprintf("**Prize:** %s\n\n", problem.Prize))
	}
	if len(problem.Tags) > 0 {
		doc.WriteString(fmt.Sprintf("**Tags:** %s\n\n", strings.Join(problem.Tags, ", ")))
	}
	doc.WriteString("---\n\n")
	doc.WriteString(solution)
	doc.WriteString("\n")

	if err := os.WriteFile(path, []byte(doc.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write solution file: %w", err)
	}

	return path, nil
}

// nextAttemptID scans the solutions directory to find the next available attempt number.
func (s *Solver) nextAttemptID(problemNumber string) int {
	prefix := problemNumber + "-"
	entries, err := os.ReadDir(s.solnsDir)
	if err != nil {
		return 1
	}

	maxID := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".md") {
			// Extract the attempt number from filename like "42-3.md"
			trimmed := strings.TrimPrefix(name, prefix)
			trimmed = strings.TrimSuffix(trimmed, ".md")
			var id int
			if _, err := fmt.Sscanf(trimmed, "%d", &id); err == nil {
				if id > maxID {
					maxID = id
				}
			}
		}
	}

	return maxID + 1
}
