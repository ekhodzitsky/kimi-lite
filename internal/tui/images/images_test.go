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

func TestCapabilityString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		cap  Capability
		want string
	}{
		{None, "none"},
		{Sixel, "sixel"},
		{Kitty, "kitty"},
		{Iterm2, "iterm2"},
		{Capability(99), "none"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.cap.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlaceholder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		part api.ContentPart
		want string
	}{
		{"image url", api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "http://example.com/a.png"}}, "🖼️ image: http://example.com/a.png"},
		{"image url nil", api.ContentPart{Type: api.ContentPartImageURL}, "🖼️ image"},
		{"image url empty", api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{}}, "🖼️ image"},
		{"image data", api.ContentPart{Type: api.ContentPartImageData, ImageData: &api.ImageData{MIMEType: "image/png"}}, "🖼️ image [image/png]"},
		{"image data nil", api.ContentPart{Type: api.ContentPartImageData}, "🖼️ image"},
		{"image data empty mime", api.ContentPart{Type: api.ContentPartImageData, ImageData: &api.ImageData{}}, "🖼️ image"},
		{"unsupported type", api.ContentPart{Type: api.ContentPartText}, "🖼️ image"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Placeholder(tc.part); got != tc.want {
				t.Errorf("Placeholder() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPartIdent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		part api.ContentPart
		want string
	}{
		{"url", api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "http://x"}}, "http://x"},
		{"url nil", api.ContentPart{Type: api.ContentPartImageURL}, ""},
		{"data", api.ContentPart{Type: api.ContentPartImageData, ImageData: &api.ImageData{Data: "abc"}}, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{"data nil", api.ContentPart{Type: api.ContentPartImageData}, ""},
		{"other", api.ContentPart{Type: api.ContentPartText}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := partIdent(tc.part); got != tc.want {
				t.Errorf("partIdent() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadPartUnsupported(t *testing.T) {
	t.Parallel()

	_, _, err := loadPart(api.ContentPart{Type: api.ContentPartText})
	if err == nil {
		t.Fatal("expected error for unsupported content part")
	}
}

func TestLoadImageURL(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	pngPath := filepath.Join(tmp, "red.png")
	if err := os.WriteFile(pngPath, makePNG(t, 2, 2), 0o600); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"nil url", "", true},
		{"data url valid", fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))), false},
		{"data url invalid missing comma", "data:image/png;base64", true},
		{"data url invalid base64", "data:image/png;base64,!!!", true},
		{"data url unescape error", "data:image/svg+xml,%ZZ", true},
		{"remote http", "https://example.com/img.png", true},
		{"remote ftp", "ftp://example.com/img.png", true},
		{"relative path", "img.png", true},
		{"missing absolute", "/does/not/exist.png", true},
		{"absolute png", pngPath, false},
		{"file scheme", "file://" + pngPath, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := loadImageURL(&api.ImageURL{URL: tc.url})
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadImageData(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		data    *api.ImageData
		wantErr bool
	}{
		{"nil", nil, true},
		{"invalid base64", &api.ImageData{Data: "!!!", MIMEType: "image/png"}, true},
		{"valid", &api.ImageData{Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1)), MIMEType: "image/png"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := loadImageData(tc.data)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSupportedFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mime string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/jpg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"Image/PNG", true},
		{"image/svg+xml", false},
		{"text/plain", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.mime, func(t *testing.T) {
			t.Parallel()
			if got := supportedFormat(tc.mime); got != tc.want {
				t.Errorf("supportedFormat(%q) = %v, want %v", tc.mime, got, tc.want)
			}
		})
	}
}

func TestClipboardMIMEForPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want string
	}{
		{"a.PNG", "image/png"},
		{"a.jpg", "image/jpeg"},
		{"a.JPEG", "image/jpeg"},
		{"a.gif", "image/gif"},
		{"a.webp", "image/webp"},
		{"a.txt", "application/octet-stream"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := clipboardMIMEForPath(tc.path); got != tc.want {
				t.Errorf("clipboardMIMEForPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestParseDataURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"not data", "https://example.com", true},
		{"missing comma", "data:image/png;base64", true},
		{"plain", "data:text/plain,hello", false},
		{"plain unescape error", "data:text/plain,%ZZ", true},
		{"base64 valid", fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString([]byte("pngbytes"))), false},
		{"base64 invalid", "data:image/png;base64,!!!", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := parseDataURL(tc.url)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestChunkString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		s    string
		size int
		want []string
	}{
		{"zero size", "abc", 0, []string{"abc"}},
		{"negative size", "abc", -1, []string{"abc"}},
		{"exact", "abcdef", 2, []string{"ab", "cd", "ef"}},
		{"remainder", "abcde", 2, []string{"ab", "cd", "e"}},
		{"empty", "", 2, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := chunkString(tc.s, tc.size)
			if len(got) != len(tc.want) {
				t.Fatalf("chunkString() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("chunk %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRenderNilAndInvalid(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Iterm2)

	cases := []struct {
		name          string
		part          api.ContentPart
		maxWidthCells int
	}{
		{"nil image url", api.ContentPart{Type: api.ContentPartImageURL}, 80},
		{"nil image data", api.ContentPart{Type: api.ContentPartImageData}, 80},
		{"decode error", api.ContentPart{Type: api.ContentPartImageData, ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("not png"))}}, 80},
		{"unsupported mime", api.ContentPart{Type: api.ContentPartImageData, ImageData: &api.ImageData{MIMEType: "image/svg+xml", Data: base64.StdEncoding.EncodeToString([]byte("<svg/>"))}}, 80},
		{"invalid base64", api.ContentPart{Type: api.ContentPartImageData, ImageData: &api.ImageData{MIMEType: "image/png", Data: "!!!"}}, 80},
		{"zero width", api.ContentPart{Type: api.ContentPartImageData, ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))}}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := r.Render(tc.part, tc.maxWidthCells, 10)
			if !strings.Contains(got, "🖼️ image") {
				t.Errorf("expected placeholder, got %q", got)
			}
		})
	}
}

func TestRenderUnsupportedTypeReturnsEmpty(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Iterm2)
	got := r.Render(api.ContentPart{Type: api.ContentPartText}, 80, 30)
	if got != "" {
		t.Errorf("expected empty render for non-image type, got %q", got)
	}
}

func TestRenderMaxInputBytes(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	bigPath := filepath.Join(tmp, "big.png")
	// maxInputBytes is 10 MiB; write a few extra bytes.
	if err := os.WriteFile(bigPath, bytes.Repeat([]byte{0}, maxInputBytes+1024), 0o600); err != nil {
		t.Fatalf("write big file: %v", err)
	}

	r := NewRenderer(Iterm2)
	part := api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: bigPath}}
	got := r.Render(part, 80, 30)
	if !strings.Contains(got, "🖼️ image") {
		t.Errorf("expected placeholder for oversized input, got %q", got)
	}
}

func TestRenderCaching(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Iterm2)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}

	first := r.Render(part, 80, 30)
	second := r.Render(part, 80, 30)
	if first != second {
		t.Error("cached render should be deterministic")
	}

	rend := r.(*renderer)
	if len(rend.cache) != 1 {
		t.Errorf("expected 1 cache entry, got %d", len(rend.cache))
	}
}

func TestRenderIterm2FormatDetails(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Iterm2)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}
	got := r.Render(part, 10, 5)

	if !strings.HasPrefix(got, "\x1b]1337;File=") {
		t.Errorf("expected OSC 1337 prefix, got %q", got)
	}
	if !strings.Contains(got, "inline=1") {
		t.Error("expected inline=1")
	}
	if !strings.Contains(got, "width=80px") {
		t.Error("expected width=80px")
	}
	if !strings.HasSuffix(got, "\x07") {
		t.Error("expected BEL suffix")
	}
}

func TestRenderKittyFormatDetails(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Kitty)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}
	got := r.Render(part, 10, 5)

	want := fmt.Sprintf("\x1b_Ga=T,f=100,t=d,c=10,r=5;%s\x1b\\", base64.StdEncoding.EncodeToString(makePNG(t, 1, 1)))
	if got != want {
		t.Errorf("Kitty output mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderKittyChunkedBoundaries(t *testing.T) {
	t.Parallel()

	r := NewRenderer(Kitty)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makeNoisePNG(t, 300, 300))},
	}
	got := r.Render(part, 80, 30)

	chunks := strings.Split(got, "\x1b_G")
	// First element is empty because the string starts with ESC_G.
	if len(chunks) < 3 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks)-1)
	}
	if !strings.HasPrefix(chunks[1], "a=T,f=100,t=d,m=1,") {
		t.Errorf("first chunk should start a new transmission, got %q", chunks[1])
	}
	last := chunks[len(chunks)-1]
	if !strings.HasPrefix(last, "m=0;") {
		t.Errorf("last chunk should end transmission, got %q", last)
	}
}

func makeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

func TestRenderSixelMissingCommands(t *testing.T) {
	// Ensure neither img2sixel nor chafa is on PATH.
	t.Setenv("PATH", t.TempDir())

	r := NewRenderer(Sixel)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}
	got := r.Render(part, 80, 30)
	if !strings.Contains(got, "🖼️ image") {
		t.Errorf("expected placeholder when sixel commands are missing, got %q", got)
	}
}

func TestRenderSixelFirstCommandSuccess(t *testing.T) {
	tmp := t.TempDir()
	makeScript(t, tmp, "img2sixel", `printf 'SIXEL_OK'`)
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	r := NewRenderer(Sixel)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}
	got := r.Render(part, 80, 30)
	if !strings.Contains(got, "SIXEL_OK") {
		t.Errorf("expected sixel command output, got %q", got)
	}
}

func TestRenderSixelFallbackCommand(t *testing.T) {
	tmp := t.TempDir()
	makeScript(t, tmp, "img2sixel", `exit 1`)
	makeScript(t, tmp, "chafa", `printf 'CHafa_OK'`)
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	r := NewRenderer(Sixel)
	part := api.ContentPart{
		Type:      api.ContentPartImageData,
		ImageData: &api.ImageData{MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(makePNG(t, 1, 1))},
	}
	got := r.Render(part, 80, 30)
	if !strings.Contains(got, "CHafa_OK") {
		t.Errorf("expected fallback command output, got %q", got)
	}
}

func TestRunSixelCmd(t *testing.T) {
	tmp := t.TempDir()
	makeScript(t, tmp, "ok-cmd", `printf 'ok'`)
	makeScript(t, tmp, "err-cmd", `echo error >&2; exit 1`)
	t.Setenv("PATH", tmp+":"+os.Getenv("PATH"))

	t.Run("success", func(t *testing.T) {
		out, err := runSixelCmd(t.Context(), "ok-cmd", nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(out) != "ok" {
			t.Errorf("output = %q, want ok", out)
		}
	})

	t.Run("stderr", func(t *testing.T) {
		_, err := runSixelCmd(t.Context(), "err-cmd", nil, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "error") {
			t.Errorf("error should mention stderr, got %v", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, err := runSixelCmd(t.Context(), "__definitely_missing__", nil, nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
