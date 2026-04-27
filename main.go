package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/demirbey05/erdos-agent/internal/keystore"
	"github.com/demirbey05/erdos-agent/internal/models"
	"github.com/demirbey05/erdos-agent/internal/scraper"
	"github.com/demirbey05/erdos-agent/internal/solver"
)

const banner = `
╔══════════════════════════════════════════════════╗
║         🧮  ERDŐS PROBLEM SOLVER AGENT  🧮       ║
║                                                  ║
║  Fetches open Erdős problems, sends them to an   ║
║  LLM for proof attempts, and saves solutions.    ║
╚══════════════════════════════════════════════════╝
`

const solnsDir = "solns"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Print(banner)

	// ── Step 1: API Key Setup ───────────────────────────────────
	cfg, err := ensureConfig()
	if err != nil {
		fatalf("Configuration error: %v", err)
	}
	fmt.Println("✓ API key configured")

	// ── Step 2: Fetch Problems ──────────────────────────────────
	fmt.Println("\n⏳ Fetching Erdős problems from GitHub...")
	problems, err := scraper.FetchProblems()
	if err != nil {
		fatalf("Failed to fetch problems: %v", err)
	}
	fmt.Printf("✓ Loaded %d problems\n", len(problems))

	// ── Step 3: Filter & Display ────────────────────────────────
	openProblems := scraper.FilterOpenProblems(problems)
	fmt.Printf("✓ %d open problems found\n\n", len(openProblems))

	displayProblems(openProblems)

	// ── Step 4: Initialize Solver ───────────────────────────────
	s, err := solver.New(cfg.Provider, cfg.Model, cfg.APIKey, solnsDir)
	if err != nil {
		fatalf("Failed to initialize solver: %v", err)
	}

	// ── Step 5: Solve Loop ──────────────────────────────────────
	for {
		selected, shouldExit := promptSelection(openProblems)
		if shouldExit {
			fmt.Println("Exiting.")
			return
		}

		if len(selected) == 0 {
			fmt.Println("No valid problems selected. Please try again.")
			continue
		}

		for _, problem := range selected {
			solveProblem(ctx, s, problem)
		}

		fmt.Println("\n✅ Done solving selected problems! Check the solns/ directory for results.")
	}
}

// ensureConfig loads or prompts for API configuration.
func ensureConfig() (keystore.StoredConfig, error) {
	if keystore.Exists() {
		cfg, err := keystore.Load()
		if err == nil {
			fmt.Printf("📋 Using stored config: provider=%s, model=%s\n", cfg.Provider, cfg.Model)
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("   Use this config? [Y/n]: ")
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer == "" || answer == "y" || answer == "yes" {
				return cfg, nil
			}
		}
	}

	return promptForConfig()
}

// promptForConfig asks the user for provider, model, and API key.
func promptForConfig() (keystore.StoredConfig, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\n── LLM Provider Setup ─────────────────────────────")
	fmt.Printf("Supported providers: %s\n", strings.Join(solver.SupportedProviders, ", "))
	fmt.Print("Provider: ")
	provider, _ := reader.ReadString('\n')
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		provider = "openai"
	}

	if !solver.IsValidProvider(provider) {
		return keystore.StoredConfig{}, fmt.Errorf("unsupported provider %q. Supported providers: %s",
			provider, strings.Join(solver.SupportedProviders, ", "))
	}

	defaultModel := defaultModelFor(provider)
	fmt.Printf("Model [%s]: ", defaultModel)
	model, _ := reader.ReadString('\n')
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultModel
	}

	fmt.Print("API Key (input hidden): ")
	keyBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // newline after hidden input
	if err != nil {
		return keystore.StoredConfig{}, fmt.Errorf("failed to read API key: %w", err)
	}

	apiKey := strings.TrimSpace(string(keyBytes))
	if apiKey == "" {
		return keystore.StoredConfig{}, fmt.Errorf("API key cannot be empty")
	}

	cfg := keystore.StoredConfig{
		Provider: provider,
		Model:    model,
		APIKey:   apiKey,
	}

	if err := keystore.Save(cfg); err != nil {
		fmt.Printf("⚠ Warning: could not save config: %v\n", err)
		fmt.Println("  (continuing with in-memory config)")
	} else {
		fmt.Println("✓ Config saved securely to ~/.erdos-agent/config.enc")
	}

	return cfg, nil
}

// defaultModelFor returns a sensible default model for each provider.
func defaultModelFor(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "claude-sonnet-4-20250514"
	case "gemini":
		return "gemini-2.5-pro"
	case "groq":
		return "llama-3.3-70b-versatile"
	case "ollama":
		return "llama3"
	default:
		return "gpt-4o"
	}
}

// displayProblems prints a table of open problems with prize info.
func displayProblems(problems []models.Problem) {
	fmt.Println("┌───────┬────────────┬──────────────────────────────────────────┐")
	fmt.Println("│  #    │   Prize    │ Tags                                     │")
	fmt.Println("├───────┼────────────┼──────────────────────────────────────────┤")

	for _, p := range problems {
		prize := p.Prize
		if !p.HasPrize() {
			prize = "  —"
		} else {
			prize = fmt.Sprintf("%-8s", prize)
		}

		tags := strings.Join(p.Tags, ", ")
		if len(tags) > 40 {
			tags = tags[:37] + "..."
		}

		fmt.Printf("│ %-5s │ %-10s │ %-40s │\n", p.Number, prize, tags)
	}

	fmt.Println("└───────┴────────────┴──────────────────────────────────────────┘")
}

// promptSelection asks the user which problems to solve.
func promptSelection(problems []models.Problem) ([]models.Problem, bool) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nEnter problem numbers to solve (comma-separated, or 'all', or 'prize'): ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return nil, true
	}

	// Build lookup map
	lookup := make(map[string]models.Problem)
	for _, p := range problems {
		lookup[p.Number] = p
	}

	if input == "all" {
		return problems, false
	}

	if input == "prize" {
		return scraper.FilterPrizeProblems(problems), false
	}

	// Parse comma-separated numbers
	var selected []models.Problem
	for _, part := range strings.Split(input, ",") {
		num := strings.TrimSpace(part)
		if num == "" {
			continue
		}
		if p, ok := lookup[num]; ok {
			selected = append(selected, p)
		} else {
			fmt.Printf("⚠ Problem #%s not found in open problems, skipping\n", num)
		}
	}

	return selected, false
}

// solveProblem fetches the description, calls the LLM, and saves the result.
func solveProblem(ctx context.Context, s *solver.Solver, problem models.Problem) {
	fmt.Printf("\n━━━ Problem #%s ", problem.Number)
	if problem.HasPrize() {
		fmt.Printf("(Prize: %s) ", problem.Prize)
	}
	fmt.Println("━━━")

	// Fetch description
	fmt.Println("  ⏳ Fetching problem description...")
	description, err := scraper.FetchProblemDescription(problem.Number)
	if err != nil {
		fmt.Printf("  ✗ Failed to fetch description: %v\n", err)
		fmt.Println("  → Using metadata only")
		description = buildFallbackDescription(problem)
	} else {
		// Truncate for display
		preview := description
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		fmt.Printf("  ✓ Description: %s\n", preview)
	}

	// Solve
	fmt.Println("  ⏳ Sending to LLM (this may take a very long time for complex proofs)...")
	startTime := time.Now()
	response, err := s.Solve(ctx, problem, description)
	elapsed := time.Since(startTime)

	if err != nil {
		// Check if the user cancelled
		if ctx.Err() != nil {
			fmt.Printf("  ✗ Operation cancelled after %v\n", elapsed.Round(time.Second))
			return
		}
		fmt.Printf("  ✗ LLM error after %v: %v\n", elapsed.Round(time.Second), err)
		return
	}

	fmt.Printf("  ✓ LLM responded in %v\n", elapsed.Round(time.Second))

	// Save
	path, err := s.SaveSolution(problem, response)
	if err != nil {
		fmt.Printf("  ✗ Failed to save solution: %v\n", err)
		return
	}

	fmt.Printf("  ✓ Solution saved → %s\n", path)
}

// buildFallbackDescription constructs a minimal description from metadata
// when the website scraper fails.
func buildFallbackDescription(p models.Problem) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Erdős Problem #%s", p.Number))
	if len(p.Tags) > 0 {
		parts = append(parts, fmt.Sprintf("Topics: %s", strings.Join(p.Tags, ", ")))
	}
	if p.Comments != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", p.Comments))
	}
	if len(p.OEIS) > 0 {
		oeis := []string{}
		for _, o := range p.OEIS {
			if o != "N/A" && o != "possible" {
				oeis = append(oeis, o)
			}
		}
		if len(oeis) > 0 {
			parts = append(parts, fmt.Sprintf("OEIS sequences: %s", strings.Join(oeis, ", ")))
		}
	}
	return strings.Join(parts, "\n")
}


func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "❌ "+format+"\n", args...)
	os.Exit(1)
}

