package core

import (
	"context"
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
