package messages

import (
	"flag"
	"io"
	"os"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"charm.land/lipgloss/v2"
)

// updateGolden, when true, regenerates golden files instead of comparing.
var updateGolden = flag.Bool("update", false, "update golden files")

// TestMain pins the lipgloss color profile to no-color so golden comparisons
// are deterministic across environments.
func TestMain(m *testing.M) {
	flag.Parse()
	lipgloss.Writer = colorprofile.NewWriter(io.Discard, []string{"NO_COLOR=1"})
	os.Exit(m.Run())
}
