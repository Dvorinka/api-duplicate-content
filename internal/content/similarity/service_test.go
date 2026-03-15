package similarity

import "testing"

func TestCompareTextHighOverlap(t *testing.T) {
	service := NewService(0, 100000)

	result, err := service.CompareText(CompareInput{
		TextA: "Go is an open source programming language built for simplicity and reliability",
		TextB: "Go is an open source programming language designed for simplicity and reliability",
	})
	if err != nil {
		t.Fatalf("compare text: %v", err)
	}

	if result.SimilarityScore < 0.5 {
		t.Fatalf("expected similarity >= 0.5, got %f", result.SimilarityScore)
	}
	if result.PlagiarismPercent < 50 {
		t.Fatalf("expected plagiarism >= 50, got %f", result.PlagiarismPercent)
	}
}

func TestCompareTextLowOverlap(t *testing.T) {
	service := NewService(0, 100000)

	result, err := service.CompareText(CompareInput{
		TextA: "saturn is a gas giant with remarkable rings",
		TextB: "bread baking requires yeast, flour, and controlled heat",
	})
	if err != nil {
		t.Fatalf("compare text: %v", err)
	}

	if result.SimilarityScore > 0.2 {
		t.Fatalf("expected low similarity, got %f", result.SimilarityScore)
	}
}

func TestNormalizeURL(t *testing.T) {
	got, err := normalizeURL("example.com/article")
	if err != nil {
		t.Fatalf("normalize url: %v", err)
	}
	if got != "https://example.com/article" {
		t.Fatalf("unexpected normalized url: %q", got)
	}
}
