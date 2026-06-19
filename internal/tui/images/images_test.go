package images

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{byte(x % 256), byte(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func makeNoisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if _, err := rand.Read(img.Pix); err != nil {
		t.Fatalf("read random pixels: %v", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestRendererPlaceholder(t *testing.T) {
	t.Parallel()

	r := NewRenderer(None)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}
	got := r.Render(part, 80, 30)
	if !strings.Contains(got, "🖼️ image") {
		t.Errorf("placeholder should contain emoji, got %q", got)
	}
}

func TestRenderIterm2Format(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Iterm2)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}
	got := r.Render(part, 80, 30)

	if !strings.HasPrefix(got, "\x1b]1337;File=") {
		t.Errorf("iTerm2 output should start with OSC 1337, got %q", got)
	}
	if !strings.Contains(got, "inline=1") {
		t.Error("iTerm2 output should contain inline=1")
	}
	if !strings.HasSuffix(got, "\x07") {
		t.Errorf("iTerm2 output should end with BEL, got %q", got)
	}
	if !strings.Contains(got, ";") && !strings.Contains(got, ":") {
		t.Error("iTerm2 output should contain key/value separators")
	}
}

func TestRenderKittyFormat(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Kitty)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}
	got := r.Render(part, 80, 30)

	if !strings.HasPrefix(got, "\x1b_G") {
		t.Errorf("Kitty output should start with APC sequence, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b\\") {
		t.Errorf("Kitty output should end with ST, got %q", got)
	}
	if !strings.Contains(got, "a=T,f=100,t=d") {
		t.Error("Kitty output should contain image metadata")
	}
}

func TestRenderKittyChunked(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Kitty)
	// A 300x300 noisy PNG produces more than 4096 bytes of base64.
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makeNoisePNG(t, 300, 300))},
	}
	got := r.Render(part, 80, 30)

	if !strings.Contains(got, "m=1") {
		t.Error("chunked Kitty output should contain continuation chunks")
	}
	if !strings.Contains(got, "m=0") {
		t.Error("chunked Kitty output should contain final chunk")
	}

	chunks := strings.Count(got, "\x1b_G")
	if chunks < 2 {
		t.Fatalf("expected multiple Kitty chunks, got %d", chunks)
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Iterm2)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/svg+xml", Data: base64.StdEncoding.EncodeToString([]byte("<svg/>"))},
	}
	got := r.Render(part, 80, 30)
	if !strings.Contains(got, "🖼️ image") {
		t.Errorf("unsupported format should fall back to placeholder, got %q", got)
	}
}

func TestRenderDataURL(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Iterm2)
	png := makePNG(t, 1, 1)
	url := fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(png))
	part := api.ContentPart{
		Type:     api.ContentPartImageURL,
		ImageURL: &api.ImageURL{URL: url},
	}
	got := r.Render(part, 80, 30)
	if !strings.HasPrefix(got, "\x1b]1337;File=") {
		t.Errorf("data URL image should render as iTerm2 sequence, got %q", got)
	}
}

func TestRenderLocalFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "red.png")
	if err := os.WriteFile(path, makePNG(t, 2, 2), 0o600); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	r := NewRenderer(Iterm2)
	part := api.ContentPart{
		Type:     api.ContentPartImageURL,
		ImageURL: &api.ImageURL{URL: path},
	}
	got := r.Render(part, 80, 30)
	if !strings.HasPrefix(got, "\x1b]1337;File=") {
		t.Errorf("local file image should render as iTerm2 sequence, got %q", got)
	}
}

func TestRenderRemoteURL(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Iterm2)
	part := api.ContentPart{
		Type:     api.ContentPartImageURL,
		ImageURL: &api.ImageURL{URL: "https://example.com/img.png"},
	}
	got := r.Render(part, 80, 30)
	if !strings.Contains(got, "🖼️ image") {
		t.Errorf("remote URL should fall back to placeholder, got %q", got)
	}
}
