// Package images renders image content parts using terminal image protocols.
package images

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"  // Register GIF decoder for image.Decode.
	_ "image/jpeg" // Register JPEG decoder for image.Decode.
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nfnt/resize"
	_ "golang.org/x/image/webp" // Register WebP decoder for image.Decode.

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	defaultCellWidth  = 8
	defaultCellHeight = 16
	maxInputBytes     = 10 * 1024 * 1024
	maxOutputBytes    = 2 * 1024 * 1024
	sixelTimeout      = 5 * time.Second
	kittyChunkSize    = 4096
)

// Renderer renders api.ContentPart image parts into terminal escape sequences.
type Renderer interface {
	Capability() Capability
	Render(part api.ContentPart, maxWidthCells, maxHeightCells int) string
}

type renderer struct {
	cap   Capability
	mu    sync.RWMutex
	cache map[cacheKey]string
}

type cacheKey struct {
	kind   api.ContentPartType
	ident  string
	width  int
	height int
	cap    Capability
}

// NewRenderer creates a Renderer for the given terminal capability.
func NewRenderer(capability Capability) Renderer {
	return &renderer{
		cap:   capability,
		cache: make(map[cacheKey]string),
	}
}

func (r *renderer) Capability() Capability { return r.cap }

// Render returns a terminal escape sequence for the image part, or a text
// placeholder when rendering is not possible or not supported.
func (r *renderer) Render(part api.ContentPart, maxWidthCells, maxHeightCells int) string {
	if part.Type != api.ContentPartImageURL && part.Type != api.ContentPartImageData {
		return ""
	}

	key := cacheKey{
		kind:   part.Type,
		ident:  partIdent(part),
		width:  maxWidthCells,
		height: maxHeightCells,
		cap:    r.cap,
	}

	r.mu.RLock()
	if cached, ok := r.cache[key]; ok {
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	rendered := r.renderUncached(part, maxWidthCells, maxHeightCells)

	r.mu.Lock()
	r.cache[key] = rendered
	r.mu.Unlock()
	return rendered
}

func (r *renderer) renderUncached(part api.ContentPart, maxWidthCells, maxHeightCells int) string {
	if r.cap == None {
		return Placeholder(part)
	}

	data, mime, err := loadPart(part)
	if err != nil {
		return Placeholder(part)
	}
	if !supportedFormat(mime) {
		return Placeholder(part)
	}
	if len(data) > maxInputBytes {
		return Placeholder(part)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Placeholder(part)
	}

	targetW := maxWidthCells * defaultCellWidth
	targetH := maxHeightCells * defaultCellHeight
	if targetW <= 0 || targetH <= 0 {
		return Placeholder(part)
	}

	thumb := resize.Thumbnail(uint(targetW), uint(targetH), img, resize.Lanczos3)

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, thumb); err != nil {
		return Placeholder(part)
	}
	if pngBuf.Len() > maxOutputBytes {
		return Placeholder(part)
	}
	pngBytes := pngBuf.Bytes()

	switch r.cap {
	case Iterm2:
		return renderIterm2(pngBytes, maxWidthCells*defaultCellWidth)
	case Kitty:
		return renderKitty(pngBytes, maxWidthCells, maxHeightCells)
	case Sixel:
		if sixel := r.renderSixel(pngBytes, maxWidthCells, maxHeightCells); sixel != "" {
			return sixel
		}
		return Placeholder(part)
	default:
		return Placeholder(part)
	}
}

// Placeholder returns a text placeholder for an image part.
func Placeholder(part api.ContentPart) string {
	switch part.Type {
	case api.ContentPartImageURL:
		if part.ImageURL != nil && part.ImageURL.URL != "" {
			return fmt.Sprintf("🖼️ image: %s", part.ImageURL.URL)
		}
		return "🖼️ image"
	case api.ContentPartImageData:
		if part.ImageData != nil && part.ImageData.MIMEType != "" {
			return fmt.Sprintf("🖼️ image [%s]", part.ImageData.MIMEType)
		}
		return "🖼️ image"
	default:
		return "🖼️ image"
	}
}

func partIdent(part api.ContentPart) string {
	switch part.Type {
	case api.ContentPartImageURL:
		if part.ImageURL != nil {
			return part.ImageURL.URL
		}
	case api.ContentPartImageData:
		if part.ImageData != nil {
			sum := sha256.Sum256([]byte(part.ImageData.Data))
			return hex.EncodeToString(sum[:])
		}
	}
	return ""
}

func loadPart(part api.ContentPart) ([]byte, string, error) {
	switch part.Type {
	case api.ContentPartImageURL:
		return loadImageURL(part.ImageURL)
	case api.ContentPartImageData:
		return loadImageData(part.ImageData)
	default:
		return nil, "", fmt.Errorf("unsupported content part type %q", part.Type)
	}
}

func loadImageURL(u *api.ImageURL) ([]byte, string, error) {
	if u == nil || u.URL == "" {
		return nil, "", fmt.Errorf("missing image URL")
	}

	raw := u.URL
	if strings.HasPrefix(raw, "data:") {
		return parseDataURL(raw)
	}

	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return nil, "", fmt.Errorf("remote image URLs are not rendered inline")
	}

	raw = strings.TrimPrefix(raw, "file://")

	if !filepath.IsAbs(raw) {
		return nil, "", fmt.Errorf("only absolute local image paths are supported")
	}

	data, err := os.ReadFile(raw) // #nosec G304 -- raw is validated as an absolute local path above
	if err != nil {
		return nil, "", fmt.Errorf("read image file: %w", err)
	}

	mime := clipboardMIMEForPath(raw)
	return data, mime, nil
}

func loadImageData(d *api.ImageData) ([]byte, string, error) {
	if d == nil {
		return nil, "", fmt.Errorf("missing image data")
	}
	data, err := base64.StdEncoding.DecodeString(d.Data)
	if err != nil {
		return nil, "", fmt.Errorf("decode image data: %w", err)
	}
	return data, d.MIMEType, nil
}

func parseDataURL(s string) ([]byte, string, error) {
	if !strings.HasPrefix(s, "data:") {
		return nil, "", fmt.Errorf("not a data URL")
	}
	s = strings.TrimPrefix(s, "data:")

	comma := strings.Index(s, ",")
	if comma < 0 {
		return nil, "", fmt.Errorf("invalid data URL")
	}

	meta := s[:comma]
	payload := s[comma+1:]

	mime := "text/plain"
	base64Encoded := false
	for _, token := range strings.Split(meta, ";") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if token == "base64" {
			base64Encoded = true
			continue
		}
		if strings.Contains(token, "/") {
			mime = token
		}
	}

	if base64Encoded {
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", fmt.Errorf("decode data URL: %w", err)
		}
		return data, mime, nil
	}

	decoded, err := url.PathUnescape(payload)
	if err != nil {
		return nil, "", fmt.Errorf("unescape data URL: %w", err)
	}
	return []byte(decoded), mime, nil
}

func supportedFormat(mime string) bool {
	m := strings.ToLower(mime)
	switch {
	case strings.HasPrefix(m, "image/png"):
		return true
	case strings.HasPrefix(m, "image/jpeg"):
		return true
	case strings.HasPrefix(m, "image/jpg"):
		return true
	case strings.HasPrefix(m, "image/gif"):
		return true
	case strings.HasPrefix(m, "image/webp"):
		return true
	}
	return false
}

func clipboardMIMEForPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func renderIterm2(png []byte, widthPx int) string {
	enc := base64.StdEncoding.EncodeToString(png)
	return fmt.Sprintf("\x1b]1337;File=inline=1;width=%dpx:%s\x07", widthPx, enc)
}

func renderKitty(png []byte, cols, rows int) string {
	enc := base64.StdEncoding.EncodeToString(png)
	if len(enc) <= kittyChunkSize {
		return fmt.Sprintf("\x1b_Ga=T,f=100,t=d,c=%d,r=%d;%s\x1b\\", cols, rows, enc)
	}

	var b strings.Builder
	chunks := chunkString(enc, kittyChunkSize)
	for i, chunk := range chunks {
		if i == 0 {
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,t=d,m=1,c=%d,r=%d;%s\x1b\\", cols, rows, chunk)
		} else if i == len(chunks)-1 {
			fmt.Fprintf(&b, "\x1b_Gm=0;%s\x1b\\", chunk)
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=1;%s\x1b\\", chunk)
		}
	}
	return b.String()
}

func chunkString(s string, size int) []string {
	if size <= 0 {
		return []string{s}
	}
	var chunks []string
	for len(s) > size {
		chunks = append(chunks, s[:size])
		s = s[size:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}

func (r *renderer) renderSixel(png []byte, cols, rows int) string {
	ctx, cancel := context.WithTimeout(context.Background(), sixelTimeout)
	defer cancel()

	widthPx := cols * defaultCellWidth
	heightPx := rows * defaultCellHeight

	if out, err := runSixelCmd(ctx, "img2sixel", []string{
		"--width", strconv.Itoa(widthPx),
		"--height", strconv.Itoa(heightPx),
		"-",
	}, png); err == nil {
		return string(out)
	}

	if out, err := runSixelCmd(ctx, "chafa", []string{
		"--format", "sixel",
		"--size", fmt.Sprintf("%dx%d", cols, rows),
		"-",
	}, png); err == nil {
		return string(out)
	}

	return ""
}

func runSixelCmd(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, fmt.Errorf("lookup %s: %w", name, err)
	}
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- name and args are hard-coded Sixel converters
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s: %s", name, bytes.TrimSpace(ee.Stderr))
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}
