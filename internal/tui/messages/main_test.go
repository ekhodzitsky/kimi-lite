package messages

import (
	"flag"
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// updateGolden, when true, regenerates golden files instead of comparing.
var updateGolden = flag.Bool("update", false, "update golden files")

// TestMain pins the lipgloss color profile to ASCII so golden comparisons are
// deterministic across environments.
func TestMain(m *testing.M) {
	flag.Parse()
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}
