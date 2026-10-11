package mcp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// GetTextElements printed its two maps in Go's random iteration order, so two
// identical calls gave differently ordered answers.
func TestFormatTextElementsIsSortedByKey(t *testing.T) {
	te := &adt.TextElements{Program: "ZRPTEXTPT_SPLIT", Language: "E",
		SelectionTexts: map[string]string{}, TextSymbols: map[string]string{}}
	for i := 19; i >= 1; i-- {
		te.SelectionTexts[fmt.Sprintf("P_%02d", i)] = "sel"
		te.TextSymbols[fmt.Sprintf("%03d", i)] = "sym"
	}
	want := formatTextElements(te)
	var prev string
	for _, line := range strings.Split(want, "\n") {
		if !strings.HasPrefix(line, "  P_") {
			continue
		}
		if line < prev {
			t.Fatalf("selection texts not sorted: %q after %q", line, prev)
		}
		prev = line
	}
	for i := 0; i < 20; i++ {
		if got := formatTextElements(te); got != want {
			t.Fatalf("call %d differs:\n%s\nvs\n%s", i, got, want)
		}
	}
}
