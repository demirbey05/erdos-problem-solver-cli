package solver

import (
	"context"
	"errors"
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

// SupportedProviders lists the valid LLM provider names.
var SupportedProviders = []string{"openai", "anthropic", "groq", "gemini", "ollama"}

// llmTimeout is the maximum time to wait for an LLM response.
// Mathematical proof generation can take extremely long, so we set a generous limit.
const llmTimeout = 120 * time.Minute

// maxRetries is the number of retry attempts for transient LLM errors.
const maxRetries = 3

// Solver sends problems to an LLM and saves the generated proofs.
type Solver struct {
	llm      anyllm.Provider
	model    string
	solnsDir string
}

// IsValidProvider returns true if the given provider name is supported.
func IsValidProvider(provider string) bool {
	for _, p := range SupportedProviders {
		if strings.EqualFold(p, provider) {
			return true
		}
	}
	return false
}

// New creates a Solver backed by the given provider/model/key.
func New(provider, model, apiKey, solnsDir string) (*Solver, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	model = strings.TrimSpace(model)

	if provider == "" {
		return nil, fmt.Errorf("provider cannot be empty")
	}
	if model == "" {
		return nil, fmt.Errorf("model cannot be empty")
	}
	if !IsValidProvider(provider) {
		return nil, fmt.Errorf("unsupported provider %q. Supported providers: %s",
			provider, strings.Join(SupportedProviders, ", "))
	}

	opts := []anyllm.Option{
		anyllm.WithAPIKey(apiKey),
		anyllm.WithTimeout(llmTimeout),
	}

	var llm anyllm.Provider
	var err error

	switch provider {
	case "openai":
		llm, err = openai.New(opts...)
	case "groq":
		llm, err = groq.New(opts...)
	case "anthropic":
		llm, err = anthropic.New(opts...)
	case "gemini":
		llm, err = gemini.New(opts...)
	default:
		return nil, fmt.Errorf("unsupported provider %q. Supported providers: %s",
			provider, strings.Join(SupportedProviders, ", "))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create %s LLM client: %w", provider, err)
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
// It retries on transient errors (timeouts, temporary network issues) up to maxRetries times.
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

	params := anyllm.CompletionParams{
		Model: s.model,
		Messages: []anyllm.Message{
			{Role: anyllm.RoleUser, Content: fullPrompt},
		},
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Check if context was already cancelled (e.g. user hit Ctrl+C)
		if ctx.Err() != nil {
			return "", fmt.Errorf("operation cancelled: %w", ctx.Err())
		}

		if attempt > 1 {
			backoff := time.Duration(attempt*attempt) * 10 * time.Second
			fmt.Printf("  ↻ Retry %d/%d in %v...\n", attempt, maxRetries, backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", fmt.Errorf("operation cancelled during retry backoff: %w", ctx.Err())
			}
		}

		response, err := s.llm.Completion(ctx, params)
		if err != nil {
			lastErr = err

			// Context cancelled by user (Ctrl+C) — don't retry
			if errors.Is(err, context.Canceled) {
				return "", fmt.Errorf("LLM request cancelled by user for problem %s", problem.Number)
			}

			// Context deadline exceeded — could be our timeout or a parent context
			if errors.Is(err, context.DeadlineExceeded) {
				fmt.Printf("  ⚠ LLM request timed out for problem %s (attempt %d/%d)\n",
					problem.Number, attempt, maxRetries)
				continue
			}

			// Check for transient network errors (retryable)
			errMsg := strings.ToLower(err.Error())
			if isTransientError(errMsg) {
				fmt.Printf("  ⚠ Transient error for problem %s (attempt %d/%d): %v\n",
					problem.Number, attempt, maxRetries, err)
				continue
			}

			// Non-retryable error (auth, invalid request, etc.)
			return "", fmt.Errorf("LLM generation failed for problem %s: %w", problem.Number, err)
		}

		// Validate response
		if response == nil {
			return "", fmt.Errorf("LLM returned nil response for problem %s", problem.Number)
		}
		if len(response.Choices) == 0 {
			return "", fmt.Errorf("LLM returned empty response (no choices) for problem %s", problem.Number)
		}

		resp := response.Choices[0].Message.Content
		respString, ok := resp.(string)
		if !ok {
			return "", fmt.Errorf("LLM returned unexpected response type %T for problem %s", resp, problem.Number)
		}
		if strings.TrimSpace(respString) == "" {
			return "", fmt.Errorf("LLM returned empty solution text for problem %s", problem.Number)
		}

		return respString, nil
	}

	return "", fmt.Errorf("LLM generation failed for problem %s after %d attempts: %w",
		problem.Number, maxRetries, lastErr)
}

// isTransientError checks if an error message indicates a transient/retryable failure.
func isTransientError(errMsg string) bool {
	transientPatterns := []string{
		"timeout",
		"deadline exceeded",
		"connection refused",
		"connection reset",
		"broken pipe",
		"eof",
		"temporary failure",
		"503",
		"502",
		"429",
		"rate limit",
		"overloaded",
		"server error",
		"internal server error",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}
	return false
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
