package scraper

import (
	"strings"
	"testing"
)

func TestFetchProblemDescription_NoNavText(t *testing.T) {
	// Fetch a real problem description and verify no nav text leaks through
	desc, err := FetchProblemDescription("1191")
	if err != nil {
		t.Fatalf("FetchProblemDescription failed: %v", err)
	}

	if desc == "" {
		t.Fatal("Expected non-empty description")
	}

	// These are navigation items that should NOT appear in the description
	navTexts := []string{
		"Forum Inbox Favourites",
		"Menu Inbox",
		"FAQ Prizes Problem Lists",
		"Dual View",
		"Random Solved",
		"Random Open",
		"Definitions Links",
	}

	for _, nav := range navTexts {
		if strings.Contains(desc, nav) {
			t.Errorf("Description contains navigation text %q.\nFull description:\n%s", nav, desc)
		}
	}

	// Verify actual problem content IS present
	if !strings.Contains(desc, "Sidon") {
		t.Errorf("Description should contain 'Sidon' (problem topic).\nFull description:\n%s", desc)
	}

	t.Logf("Extracted description (%d chars):\n%s", len(desc), desc)
}
