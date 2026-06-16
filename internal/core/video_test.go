package core

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func skipNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not found in PATH")
	}
}

func createTestVideo(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi",
		"-i", "testsrc=size=320x240:rate=1:duration=2",
		"-pix_fmt", "yuv420p",
		"-y",
		path,
	)
	if err := cmd.Run(); err != nil {
		t.Fatalf("create test video: %v", err)
	}
}

func TestVideoExtractor_Extract(t *testing.T) {
	skipNoFFmpeg(t)

	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "test.mp4")
	createTestVideo(t, videoPath)

	ext := NewVideoExtractor()
	if !ext.Available() {
		t.Fatal("expected extractor to be available")
	}

	info, err := ext.Extract(context.Background(), videoPath, 2)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if info.Width != 320 || info.Height != 240 {
		t.Errorf("resolution = %dx%d, want 320x240", info.Width, info.Height)
	}
	if info.DurationSeconds <= 0 {
		t.Errorf("duration = %f, want > 0", info.DurationSeconds)
	}
	if len(info.Frames) != 2 {
		t.Errorf("frames = %d, want 2", len(info.Frames))
	}
	for _, f := range info.Frames {
		if !strings.HasPrefix(f.DataURI, "data:image/png;base64,") {
			t.Errorf("frame data URI malformed: %q", f.DataURI)
		}
	}
}

func TestVideoExtractor_NotAvailable(t *testing.T) {
	ext := &VideoExtractor{ffmpegPath: "", ffprobePath: ""}
	_, err := ext.Extract(context.Background(), "video.mp4", 1)
	if err == nil {
		t.Fatal("expected error when ffmpeg missing")
	}
}

func TestVideoResultJSON_Cap(t *testing.T) {
	info := &VideoInfo{
		Path:   "video.mp4",
		Width:  320,
		Height: 240,
		Frames: []Frame{{DataURI: strings.Repeat("x", 10000)}},
	}
	out := videoResultJSON(info, 500)
	if len(out) > 500 {
		t.Errorf("output length %d exceeds cap 500", len(out))
	}
}

func TestVideoResultJSON_FallbackIsValidJSON(t *testing.T) {
	info := &VideoInfo{
		Path:   strings.Repeat("x", 1000),
		Width:  320,
		Height: 240,
		Frames: []Frame{{DataURI: strings.Repeat("y", 10000)}},
	}
	out := videoResultJSON(info, 100)
	if !strings.HasPrefix(out, `{"error"`) {
		t.Errorf("expected JSON error object, got %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Errorf("fallback is not valid JSON: %v\nout=%q", err, out)
	}
}

func TestVideoExtractor_FrameTimestampsCoverEnd(t *testing.T) {
	skipNoFFmpeg(t)

	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "test.mp4")
	createTestVideo(t, videoPath)

	ext := NewVideoExtractor()
	info, err := ext.Extract(context.Background(), videoPath, 3)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(info.Frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(info.Frames))
	}
	last := info.Frames[len(info.Frames)-1]
	if last.Timestamp != info.DurationSeconds {
		t.Errorf("last frame timestamp = %f, want %f", last.Timestamp, info.DurationSeconds)
	}
}

func TestVideoExtractor_Extract_DefaultMaxFrames(t *testing.T) {
	skipNoFFmpeg(t)

	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "test.mp4")
	createTestVideo(t, videoPath)

	ext := NewVideoExtractor()
	info, err := ext.Extract(context.Background(), videoPath, 0)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(info.Frames) != 1 {
		t.Errorf("expected 1 default frame, got %d", len(info.Frames))
	}
}

func TestVideoExtractor_Extract_ProbeError(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffprobePath := filepath.Join(binDir, "ffprobe")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	writeScript(t, ffprobePath, "#!/bin/sh\nexit 1\n")
	writeScript(t, ffmpegPath, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))

	ext := NewVideoExtractor()
	_, err := ext.Extract(context.Background(), filepath.Join(tmpDir, "video.mp4"), 1)
	if err == nil {
		t.Fatal("expected probe error")
	}
}

func TestVideoExtractor_Extract_FFmpegError(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffprobePath := filepath.Join(binDir, "ffprobe")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	writeScript(t, ffprobePath, "#!/bin/sh\nprintf '%s' '{\"format\":{\"format_name\":\"mov,mp4,m4a,3gp,3g2,mj2\",\"duration\":\"2.0\"},\"streams\":[{\"codec_type\":\"video\",\"width\":320,\"height\":240}]}'\n")
	writeScript(t, ffmpegPath, "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))

	ext := NewVideoExtractor()
	_, err := ext.Extract(context.Background(), filepath.Join(tmpDir, "video.mp4"), 1)
	if err == nil {
		t.Fatal("expected ffmpeg error")
	}
}

func TestVideoExtractor_probe_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffprobePath := filepath.Join(binDir, "ffprobe")
	writeScript(t, ffprobePath, "#!/bin/sh\necho 'not json'\n")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))

	ext := NewVideoExtractor()
	_, err := ext.probe(context.Background(), filepath.Join(tmpDir, "video.mp4"))
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestVideoExtractor_probe_NoVideoStream(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffprobePath := filepath.Join(binDir, "ffprobe")
	writeScript(t, ffprobePath, "#!/bin/sh\nprintf '%s' '{\"format\":{\"format_name\":\"mp3\",\"duration\":\"2.0\"},\"streams\":[{\"codec_type\":\"audio\"}]}'\n")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))

	ext := NewVideoExtractor()
	info, err := ext.probe(context.Background(), filepath.Join(tmpDir, "audio.mp3"))
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Width != 0 || info.Height != 0 {
		t.Errorf("expected zero dimensions for audio, got %dx%d", info.Width, info.Height)
	}
}

func TestVideoExtractor_extractFrames_NoFrames(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	// ffmpeg exits successfully but produces no png files.
	writeScript(t, ffmpegPath, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))

	ext := &VideoExtractor{ffmpegPath: ffmpegPath}
	_, err := ext.extractFrames(context.Background(), filepath.Join(tmpDir, "video.mp4"), 2.0, 1)
	if err == nil {
		t.Fatal("expected no frames error")
	}
}

func TestVideoExtractor_extractFrames_ReadDirError(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	// Remove the temp directory after creating a frame, so ReadDir fails.
	writeScript(t, ffmpegPath, "#!/bin/sh\ndir=$(dirname \"$9\")\necho fakeframe > \"$dir/frame_001.png\"\nrm -rf \"$dir\"\n")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))

	ext := &VideoExtractor{ffmpegPath: ffmpegPath}
	_, err := ext.extractFrames(context.Background(), filepath.Join(tmpDir, "video.mp4"), 2.0, 1)
	if err == nil {
		t.Fatal("expected read dir error")
	}
}

func TestVideoExtractor_extractFrames_ZeroDuration(t *testing.T) {
	skipNoFFmpeg(t)

	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "test.mp4")
	createTestVideo(t, videoPath)

	ext := NewVideoExtractor()
	// Call extractFrames directly with duration=0 to exercise the timestamp=0 path.
	frames, err := ext.extractFrames(context.Background(), videoPath, 0, 1)
	if err != nil {
		t.Fatalf("extractFrames: %v", err)
	}
	if len(frames) != 1 {
		t.Errorf("expected 1 frame, got %d", len(frames))
	}
	if frames[0].Timestamp != 0 {
		t.Errorf("timestamp = %f, want 0", frames[0].Timestamp)
	}
}

func TestVideoExtractor_lookup_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)
	ext := NewVideoExtractor()
	if ext.Available() {
		t.Error("expected extractor to be unavailable when binaries missing")
	}
}

func TestVideoExtractor_extractFrames_TempDirError(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	writeScript(t, ffmpegPath, "#!/bin/sh\nexit 0\n")

	ext := &VideoExtractor{ffmpegPath: ffmpegPath}
	t.Setenv("TMPDIR", filepath.Join(tmpDir, "does", "not", "exist"))
	_, err := ext.extractFrames(context.Background(), filepath.Join(tmpDir, "video.mp4"), 2.0, 1)
	if err == nil {
		t.Fatal("expected temp dir creation error")
	}
}

func TestVideoExtractor_extractFrames_ReadFrameError(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	writeScript(t, ffmpegPath, "#!/bin/sh\ndir=$(dirname \"$9\")\necho fakeframe > \"$dir/frame_001.png\"\nchmod 000 \"$dir/frame_001.png\"\n")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))

	ext := &VideoExtractor{ffmpegPath: ffmpegPath}
	_, err := ext.extractFrames(context.Background(), filepath.Join(tmpDir, "video.mp4"), 2.0, 1)
	if err == nil {
		t.Fatal("expected read frame error")
	}
}

func TestVideoExtractor_extractFrames_SkipsDirEntries(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	writeScript(t, ffmpegPath, "#!/bin/sh\ndir=$(dirname \"$9\")\nmkdir \"$dir/frame_001.png\"\necho fakeframe > \"$dir/frame_002.png\"\n")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+os.Getenv("PATH"))

	ext := &VideoExtractor{ffmpegPath: ffmpegPath}
	frames, err := ext.extractFrames(context.Background(), filepath.Join(tmpDir, "video.mp4"), 2.0, 2)
	if err != nil {
		t.Fatalf("extractFrames: %v", err)
	}
	if len(frames) != 1 {
		t.Errorf("expected 1 frame, got %d", len(frames))
	}
}

func TestVideoResultJSON_MarshalError(t *testing.T) {
	info := &VideoInfo{
		Path:            "video.mp4",
		DurationSeconds: math.NaN(),
		Frames:          []Frame{{DataURI: "data"}},
	}
	out := videoResultJSON(info, 100)
	if !strings.HasPrefix(out, `{"error"`) {
		t.Errorf("expected JSON error object, got %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Errorf("fallback is not valid JSON: %v", err)
	}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
}

func TestDetectMediaType_HeaderBeforeExtension(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// A PNG file saved with a .txt extension should be detected as image/png.
	pngPath := filepath.Join(root, "not-actually.txt")
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(pngPath, pngHeader, 0644); err != nil {
		t.Fatalf("write png header: %v", err)
	}
	if got := detectMediaType(pngPath); got != "image/png" {
		t.Errorf("detectMediaType(png with .txt) = %q, want image/png", got)
	}

	// An empty .mp4 file falls back to the extension.
	mp4Path := filepath.Join(root, "empty.mp4")
	if err := os.WriteFile(mp4Path, []byte{}, 0644); err != nil {
		t.Fatalf("write empty mp4: %v", err)
	}
	if got := detectMediaType(mp4Path); got != "video/mp4" {
		t.Errorf("detectMediaType(empty .mp4) = %q, want video/mp4", got)
	}
}
