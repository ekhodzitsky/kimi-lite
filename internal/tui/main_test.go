package tui

import (
	"flag"
	"io"
	"os"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// TestMain pins the lipgloss color profile to no-color so golden comparisons
// and any direct rendering assertions are deterministic across environments.
func TestMain(m *testing.M) {
	flag.Parse()
	lipgloss.Writer = colorprofile.NewWriter(io.Discard, []string{"NO_COLOR=1"})
	os.Exit(m.Run())
}
