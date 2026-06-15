package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// VideoInfo holds metadata and extracted frames for a video file.
type VideoInfo struct {
	Path            string  `json:"path"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	DurationSeconds float64 `json:"duration_seconds"`
	Format          string  `json:"format"`
	Frames          []Frame `json:"frames,omitempty"`
}

// Frame represents a single extracted video frame.
type Frame struct {
	Timestamp   float64 `json:"timestamp_seconds"`
	DataURI     string  `json:"data_uri"`
	ContentType string  `json:"content_type"`
}

// VideoExtractor uses ffmpeg/ffprobe to extract video metadata and frames.
type VideoExtractor struct {
	ffmpegPath  string
	ffprobePath string
}

// NewVideoExtractor creates a VideoExtractor. If ffmpeg or ffprobe is not found
// in PATH, the extractor returns an error from Extract.
func NewVideoExtractor() *VideoExtractor {
	return &VideoExtractor{
		ffmpegPath:  lookup("ffmpeg"),
		ffprobePath: lookup("ffprobe"),
	}
}

func lookup(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// Available reports whether ffmpeg and ffprobe are present.
func (v *VideoExtractor) Available() bool {
	return v.ffmpegPath != "" && v.ffprobePath != ""
}

// Extract returns metadata and up to maxFrames key frames from path.
// maxFrames <= 0 defaults to 1.
//
// Security note: the caller is responsible for validating the path and, when
// running inside a sandbox, copying the target to a temporary file before
// invoking ffmpeg. This function passes the provided path directly to ffmpeg
// and ffprobe, so it must already be a safe path.
func (v *VideoExtractor) Extract(ctx context.Context, path string, maxFrames int) (*VideoInfo, error) {
	if !v.Available() {
		return nil, fmt.Errorf("ffmpeg/ffprobe not found in PATH")
	}
	if maxFrames <= 0 {
		maxFrames = 1
	}

	info, err := v.probe(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("probe video: %w", err)
	}

	frames, err := v.extractFrames(ctx, path, info.DurationSeconds, maxFrames)
	if err != nil {
		return nil, fmt.Errorf("extract frames: %w", err)
	}
	info.Frames = frames
	return info, nil
}

// probe runs ffprobe and parses the JSON output.
func (v *VideoExtractor) probe(ctx context.Context, path string) (*VideoInfo, error) {
	// path is expected to be a safe, validated path before reaching the video
	// extractor (see Extract's security note).
	//nolint:gosec
	cmd := exec.CommandContext(ctx, v.ffprobePath,
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var payload struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	info := &VideoInfo{
		Path:   path,
		Format: payload.Format.FormatName,
	}
	if d, err := strconv.ParseFloat(payload.Format.Duration, 64); err == nil {
		info.DurationSeconds = d
	}
	for _, s := range payload.Streams {
		if s.CodecType == "video" {
			info.Width = s.Width
			info.Height = s.Height
			break
		}
	}
	return info, nil
}

// extractFrames runs ffmpeg to extract up to maxFrames evenly-spaced PNG frames.
func (v *VideoExtractor) extractFrames(ctx context.Context, path string, duration float64, maxFrames int) ([]Frame, error) {
	tmpDir, err := os.MkdirTemp("", "kimi-lite-video-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	pattern := filepath.Join(tmpDir, "frame_%03d.png")
	// Select evenly spaced frames by time.
	selectExpr := fmt.Sprintf("select='not(mod(n\\,%d))'", maxFrames)
	if duration > 0 {
		// Use fps filter to sample frames evenly across duration.
		fps := float64(maxFrames) / duration
		selectExpr = fmt.Sprintf("fps=%f,scale='min(1280,iw)':-1", fps)
	}

	// path is expected to be a safe, validated path (see Extract's security note).
	//nolint:gosec
	cmd := exec.CommandContext(ctx, v.ffmpegPath,
		"-i", path,
		"-vf", selectExpr,
		"-frames:v", strconv.Itoa(maxFrames),
		"-vsync", "vfr",
		pattern,
	)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("read frames dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var frames []Frame
	for i, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") {
			continue
		}
		//nolint:gosec // files are written by ffmpeg into a temp dir we control
		data, err := os.ReadFile(filepath.Join(tmpDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read frame: %w", err)
		}
		var ts float64
		if duration > 0 && maxFrames > 1 {
			ts = duration * float64(i) / float64(maxFrames-1)
		}
		frames = append(frames, Frame{
			Timestamp:   ts,
			DataURI:     fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(data)),
			ContentType: "image/png",
		})
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("ffmpeg produced no frames")
	}
	return frames, nil
}

// videoResultJSON returns a JSON string for a VideoInfo, capped in length.
func videoResultJSON(info *VideoInfo, maxBytes int) string {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err)
	}
	if len(data) > maxBytes {
		info.Frames = nil
		data, err = json.Marshal(info)
		if err != nil {
			return fmt.Sprintf(`{"error":%q}`, err)
		}
		if len(data) > maxBytes {
			return fmt.Sprintf(`{"error":"video result exceeds maximum size of %d bytes"}`, maxBytes)
		}
	}
	return string(data)
}
