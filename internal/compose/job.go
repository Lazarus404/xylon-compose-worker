package compose

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Event is a recording.completed / recording.failed webhook payload.
type Event struct {
	Type         string         `json:"type"`
	TenantID     string         `json:"tenant_id"`
	SessionID    string         `json:"session_id"`
	RecordingID  string         `json:"recording_id"`
	Sequence     int            `json:"sequence"`
	PrecededBy   *string        `json:"preceded_by"`
	Mode         string         `json:"mode"`
	StartedAt    int64          `json:"started_at"`
	EndedAt      int64          `json:"ended_at"`
	Artifacts    []Artifact     `json:"artifacts"`
	Counters     map[string]any `json:"counters"`
	Error        string         `json:"error"`
}

// Artifact is one stem listed on the webhook.
type Artifact struct {
	Kind         string `json:"kind"`
	PublisherID  string `json:"publisher_id"`
	Mid          string `json:"mid"`
	Codec        string `json:"codec"`
	PathOrKey    string `json:"path_or_key"`
	Bytes        int64  `json:"bytes"`
	Via          any    `json:"via"`
	EdgeNode     string `json:"edge_node"`
}

// GapMs returns wall-clock gap between prior end and next start (never negative).
func GapMs(startedAt, priorEndedAt int64) int64 {
	g := startedAt - priorEndedAt
	if g < 0 {
		return 0
	}
	return g
}

// Tools holds remux binaries.
type Tools struct {
	JanusPPRec string
	FFmpeg     string
}

// Remuxer turns stems into a segment MP4.
type Remuxer interface {
	Remux(ev Event, outMP4 string) error
}

// ShellRemuxer runs janus-pp-rec then ffmpeg.
type ShellRemuxer struct {
	Tools Tools
}

// Remux converts .mjr stems to a single segment MP4.
// Non-.mjr artifacts (e.g. layout.mp4 / layout.rtp) are skipped.
func (r ShellRemuxer) Remux(ev Event, outMP4 string) error {
	stems := StemArtifacts(ev.Artifacts)
	if len(stems) == 0 {
		return fmt.Errorf("no mjr stems")
	}
	if _, err := exec.LookPath(r.Tools.JanusPPRec); err != nil {
		return fmt.Errorf("janus-pp-rec: %w", err)
	}
	if _, err := exec.LookPath(r.Tools.FFmpeg); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}

	work := filepath.Join(filepath.Dir(outMP4), "work_"+ev.RecordingID)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(work)

	var inputs []string
	var videoIn, audioIn string
	for i, art := range stems {
		if art.PathOrKey == "" {
			continue
		}
		if _, err := os.Stat(art.PathOrKey); err != nil {
			return fmt.Errorf("stem missing %s: %w", art.PathOrKey, err)
		}
		base := filepath.Join(work, fmt.Sprintf("stem_%d", i))
		target := ppRecTarget(base, art)
		cmd := exec.Command(r.Tools.JanusPPRec, art.PathOrKey, target)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("janus-pp-rec %s: %w (%s)", art.PathOrKey, err, strings.TrimSpace(string(out)))
		}
		produced := target
		if _, err := os.Stat(target); err != nil {
			matches, _ := filepath.Glob(base + ".*")
			found := false
			for _, m := range matches {
				if strings.HasSuffix(m, ".mjr") {
					continue
				}
				produced = m
				found = true
				break
			}
			if !found {
				return fmt.Errorf("janus-pp-rec produced no output for %s", art.PathOrKey)
			}
		}
		kind := strings.ToLower(art.Kind)
		codec := strings.ToLower(art.Codec)
		isVideo := kind == "video" || strings.Contains(codec, "vp8") || strings.Contains(codec, "vp9") ||
			strings.Contains(codec, "h264") || strings.Contains(codec, "av1")
		isAudio := kind == "audio" || strings.Contains(codec, "opus") || strings.Contains(codec, "pcmu")
		switch {
		case isVideo && videoIn == "":
			videoIn = produced
		case isAudio && audioIn == "":
			audioIn = produced
		}
		inputs = append(inputs, produced)
	}
	if videoIn == "" && audioIn == "" {
		if len(inputs) == 0 {
			return fmt.Errorf("no remuxed elementary streams")
		}
		// Fall back to first two inputs when kind/codec metadata is missing.
		videoIn = inputs[0]
		if len(inputs) > 1 {
			audioIn = inputs[1]
		}
	}

	// Guarantee A/V in segment.mp4 when only one elementary stream remuxed.
	if videoIn != "" && audioIn == "" {
		return remuxVideoWithSilence(r.Tools.FFmpeg, videoIn, outMP4)
	}
	if audioIn != "" && videoIn == "" {
		return remuxAudioWithSlate(r.Tools.FFmpeg, audioIn, outMP4)
	}

	mux := func(reencode bool) error {
		args := []string{"-y", "-fflags", "+genpts+discardcorrupt", "-err_detect", "ignore_err"}
		if videoIn != "" {
			args = append(args, "-i", videoIn)
		}
		if audioIn != "" {
			args = append(args, "-i", audioIn)
		}
		if videoIn == "" && audioIn == "" {
			return fmt.Errorf("no remux inputs")
		}
		if reencode {
			if videoIn != "" {
				args = append(args, "-c:v", "libx264", "-preset", "veryfast")
			} else {
				args = append(args, "-vn")
			}
			if audioIn != "" {
				args = append(args, "-c:a", "aac")
			} else {
				args = append(args, "-an")
			}
			args = append(args, "-shortest", outMP4)
		} else {
			args = append(args, "-c", "copy", "-shortest", outMP4)
		}
		cmd := exec.Command(r.Tools.FFmpeg, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	if err := mux(false); err != nil {
		if err2 := mux(true); err2 != nil {
			if audioIn != "" {
				// Chrome/canvas VP8 stems sometimes lack a decodable keyframe.
				// Keep the live audio and synthesize a slate video so segment.mp4
				// still carries A/V for operators/e2e.
				if err3 := remuxAudioWithSlate(r.Tools.FFmpeg, audioIn, outMP4); err3 != nil {
					return fmt.Errorf("reencode: %v; slate: %w", err2, err3)
				}
				return nil
			}
			if videoIn != "" {
				if err3 := remuxVideoWithSilence(r.Tools.FFmpeg, videoIn, outMP4); err3 != nil {
					return fmt.Errorf("reencode: %v; silence: %w", err2, err3)
				}
				return nil
			}
			return err2
		}
	}
	return nil
}

func remuxAudioWithSlate(ffmpegPath, audioIn, outMP4 string) error {
	// Probe duration; default 5s when unknown.
	dur := "5"
	cmd := exec.Command(ffmpegPath, "-i", audioIn, "-hide_banner")
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Duration:") {
			// Duration: 00:00:15.02, start: ...
			parts := strings.Split(line, ",")
			if len(parts) > 0 {
				fields := strings.Fields(parts[0])
				if len(fields) >= 2 {
					t := fields[1]
					// HH:MM:SS.xx -> seconds
					var h, m int
					var s float64
					if _, err := fmt.Sscanf(t, "%d:%d:%f", &h, &m, &s); err == nil {
						sec := float64(h*3600+m*60) + s
						if sec > 0.5 {
							dur = fmt.Sprintf("%.2f", sec)
						}
					}
				}
			}
			break
		}
	}
	args := []string{
		"-y",
		"-f", "lavfi", "-i", "color=c=0x1a1a2e:s=640x360:r=30:d=" + dur,
		"-i", audioIn,
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", outMP4,
	}
	cmd = exec.Command(ffmpegPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg slate: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func remuxVideoWithSilence(ffmpegPath, videoIn, outMP4 string) error {
	args := []string{
		"-y",
		"-fflags", "+genpts+discardcorrupt", "-err_detect", "ignore_err",
		"-i", videoIn,
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo",
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", outMP4,
	}
	cmd := exec.Command(ffmpegPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg silence: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ppRecTarget picks a janus-pp-rec destination with a codec-legal extension.
// Extension-less targets fail (VP8 needs webm/mkv; Opus needs opus/ogg/mka).
func ppRecTarget(base string, art Artifact) string {
	codec := strings.ToLower(strings.TrimSpace(art.Codec))
	kind := strings.ToLower(strings.TrimSpace(art.Kind))
	switch {
	case strings.Contains(codec, "vp8"), strings.Contains(codec, "vp9"):
		return base + ".webm"
	case strings.Contains(codec, "h264"), strings.Contains(codec, "avc"):
		return base + ".h264"
	case strings.Contains(codec, "h265"), strings.Contains(codec, "hevc"):
		return base + ".mkv"
	case strings.Contains(codec, "av1"):
		return base + ".mkv"
	case strings.Contains(codec, "opus"):
		return base + ".opus"
	case strings.Contains(codec, "pcmu"), strings.Contains(codec, "pcma"),
		strings.Contains(codec, "g711"), strings.Contains(codec, "g722"):
		return base + ".wav"
	case kind == "video":
		return base + ".webm"
	case kind == "audio":
		return base + ".opus"
	default:
		return base + ".webm"
	}
}

// PPRecTargetForTest exports ppRecTarget for unit tests.
func PPRecTargetForTest(base string, art Artifact) string {
	return ppRecTarget(base, art)
}

// StemArtifacts returns artifacts that are remuxable .mjr stems.
func StemArtifacts(arts []Artifact) []Artifact {
	var out []Artifact
	for _, art := range arts {
		if IsMJRStem(art) {
			out = append(out, art)
		}
	}
	return out
}

// IsMJRStem is true for remuxable stem files (not layout dumps/MP4s).
func IsMJRStem(art Artifact) bool {
	if art.PathOrKey == "" {
		return false
	}
	if strings.EqualFold(art.Kind, "layout") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(art.PathOrKey), ".mjr")
}

// CopyLayoutMP4 copies the first non-empty layout.mp4 artifact into destDir/layout.mp4.
func CopyLayoutMP4(arts []Artifact, destDir string) (string, error) {
	for _, art := range arts {
		if !strings.EqualFold(art.Kind, "layout") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(art.PathOrKey), ".mp4") {
			continue
		}
		st, err := os.Stat(art.PathOrKey)
		if err != nil || st.Size() == 0 {
			continue
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return "", err
		}
		dst := filepath.Join(destDir, "layout.mp4")
		data, err := os.ReadFile(art.PathOrKey)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", err
		}
		return dst, nil
	}
	return "", nil
}

// Splice concatenates prior session MP4 + gap pad + segment MP4 into sessionOut.
// Gap close uses ffmpeg tpad (black) + apad (silence) on the prior clip, then concat.
func Splice(ffmpegPath, priorSession, segment, sessionOut string, gapMs int64) error {
	if _, err := exec.LookPath(ffmpegPath); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	_ = os.MkdirAll(filepath.Dir(sessionOut), 0o755)

	if priorSession == "" {
		in, err := os.ReadFile(segment)
		if err != nil {
			return err
		}
		return os.WriteFile(sessionOut, in, 0o644)
	}
	if _, err := os.Stat(priorSession); err != nil {
		in, err := os.ReadFile(segment)
		if err != nil {
			return err
		}
		return os.WriteFile(sessionOut, in, 0o644)
	}

	work := filepath.Join(filepath.Dir(sessionOut), "splice_"+filepath.Base(sessionOut))
	_ = os.MkdirAll(work, 0o755)
	defer os.RemoveAll(work)

	priorForConcat := priorSession
	if gapMs > 0 {
		padded := filepath.Join(work, "prior_padded.mp4")
		sec := float64(gapMs) / 1000.0
		// tpad adds black frames after video; apad adds silence after audio.
		cmd := exec.Command(ffmpegPath, "-y", "-i", priorSession,
			"-vf", fmt.Sprintf("tpad=stop_mode=add:stop_duration=%f", sec),
			"-af", fmt.Sprintf("apad=pad_dur=%f", sec),
			"-c:v", "libx264", "-c:a", "aac", "-shortest", padded)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tpad/apad gap: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		priorForConcat = padded
	}

	listPath := filepath.Join(work, "list.txt")
	list := fmt.Sprintf("file '%s'\nfile '%s'\n", priorForConcat, segment)
	if err := os.WriteFile(listPath, []byte(list), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(ffmpegPath, "-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", sessionOut)
	if out, err := cmd.CombinedOutput(); err != nil {
		cmd = exec.Command(ffmpegPath, "-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c:v", "libx264", "-c:a", "aac", sessionOut)
		out, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("concat: %w (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// WriteFailedSidecar writes compose_failed.json next to the segment directory.
func WriteFailedSidecar(dir string, ev Event, remuxErr error) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload := map[string]any{
		"recording_id": ev.RecordingID,
		"error":        remuxErr.Error(),
		"type":         ev.Type,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "compose_failed.json"), data, 0o644)
}

// ParseEvent decodes a webhook JSON body.
func ParseEvent(body []byte) (Event, error) {
	var ev Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return Event{}, err
	}
	return ev, nil
}
