package subtitle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

var (
	srtTimeRegex = regexp.MustCompile(`(?m)^([0-9]{2}:[0-9]{2}:[0-9]{2}),([0-9]{3})\s*-->\s*([0-9]{2}:[0-9]{2}:[0-9]{2}),([0-9]{3})(.*)$`)
	vttTimeRegex = regexp.MustCompile(`(?m)^([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3})\s*-->\s*([0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3})(.*)$`)
	assTagRegex  = regexp.MustCompile(`\{[^}]*\}`)
)

// DecodeToUTF8 converts raw subtitle bytes from UTF-8, GBK/GB18030, or Big5 into a clean UTF-8 string.
func DecodeToUTF8(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("empty subtitle data")
	}

	// 1. Strip UTF-8 BOM if present.
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		raw = raw[3:]
	}

	// 2. Check if it's already valid UTF-8 without excessive replacement chars.
	if utf8.Valid(raw) {
		text := string(raw)
		// If valid UTF-8 and does not contain abnormal replacement characters, return directly.
		if !strings.Contains(text, "\uFFFD") {
			return text, nil
		}
	}

	// 3. Attempt GBK / GB18030 (most common for Chinese fansub releases).
	gbkReader := transform.NewReader(bytes.NewReader(raw), simplifiedchinese.GB18030.NewDecoder())
	decoded, err := io.ReadAll(gbkReader)
	if err == nil && utf8.Valid(decoded) && len(decoded) > 0 {
		text := string(decoded)
		if !strings.Contains(text, "\uFFFD") {
			return text, nil
		}
	}

	// 4. Attempt Big5 (common for traditional Chinese releases).
	big5Reader := transform.NewReader(bytes.NewReader(raw), traditionalchinese.Big5.NewDecoder())
	decodedBig5, err := io.ReadAll(big5Reader)
	if err == nil && utf8.Valid(decodedBig5) && len(decodedBig5) > 0 {
		return string(decodedBig5), nil
	}

	// Fallback to lossy UTF-8 decoding.
	return string(bytes.ToValidUTF8(raw, []byte(" "))), nil
}

// ConvertToWebVTT standardizes SRT, ASS/SSA, or existing VTT text into standard WebVTT format.
func ConvertToWebVTT(content string, ext string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", errors.New("empty subtitle content")
	}

	ext = strings.ToLower(strings.TrimPrefix(ext, "."))

	switch ext {
	case "vtt":
		if strings.HasPrefix(trimmed, "WEBVTT") {
			return trimmed + "\n", nil
		}
		return "WEBVTT\n\n" + trimmed + "\n", nil

	case "ass", "ssa":
		return convertASSToVTT(trimmed)

	default: // Default to SRT or SRT-like format.
		return convertSRTToVTT(trimmed)
	}
}

// convertSRTToVTT converts an SRT format string to standard WebVTT.
func convertSRTToVTT(srt string) (string, error) {
	// Normalize CRLF to LF.
	normalized := strings.ReplaceAll(srt, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	// Replace comma in timecodes (00:01:20,123 -> 00:01:20.123).
	vttContent := srtTimeRegex.ReplaceAllString(normalized, "$1.$2 --> $3.$4$5")

	var sb strings.Builder
	sb.WriteString("WEBVTT\n\n")
	sb.WriteString(strings.TrimSpace(vttContent))
	sb.WriteString("\n")

	return sb.String(), nil
}

// convertASSToVTT extracts dialogue lines from an ASS/SSA file and produces clean WebVTT cues.
func convertASSToVTT(ass string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(ass, "\r\n", "\n"), "\n")

	inEvents := false
	formatFields := []string{}
	startIndex, endIndex, textIndex := -1, -1, -1

	type cue struct {
		start string
		end   string
		text  string
	}
	var cues []cue

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inEvents = strings.EqualFold(trimmed, "[Events]")
			continue
		}

		if !inEvents {
			continue
		}

		if strings.HasPrefix(strings.ToLower(trimmed), "format:") {
			parts := strings.Split(trimmed[7:], ",")
			formatFields = make([]string, len(parts))
			for i, p := range parts {
				field := strings.ToLower(strings.TrimSpace(p))
				formatFields[i] = field
				switch field {
				case "start":
					startIndex = i
				case "end":
					endIndex = i
				case "text":
					textIndex = i
				}
			}
			continue
		}

		if strings.HasPrefix(strings.ToLower(trimmed), "dialogue:") {
			rawDialogue := trimmed[9:]
			parts := strings.SplitN(rawDialogue, ",", len(formatFields))
			if len(parts) < len(formatFields) || startIndex == -1 || endIndex == -1 || textIndex == -1 {
				continue
			}

			startStr := strings.TrimSpace(parts[startIndex])
			endStr := strings.TrimSpace(parts[endIndex])
			rawText := parts[textIndex]

			// Strip ASS style overrides like {\pos(...)}, {\fad(...)}.
			cleanText := assTagRegex.ReplaceAllString(rawText, "")
			// Replace \N, \n with real newline.
			cleanText = strings.ReplaceAll(cleanText, `\N`, "\n")
			cleanText = strings.ReplaceAll(cleanText, `\n`, "\n")
			cleanText = strings.TrimSpace(cleanText)

			if cleanText == "" {
				continue
			}

			vttStart := formatASSTime(startStr)
			vttEnd := formatASSTime(endStr)

			cues = append(cues, cue{
				start: vttStart,
				end:   vttEnd,
				text:  cleanText,
			})
		}
	}

	var sb strings.Builder
	sb.WriteString("WEBVTT\n\n")
	for i, c := range cues {
		sb.WriteString(fmt.Sprintf("%d\n%s --> %s\n%s\n\n", i+1, c.start, c.end, c.text))
	}

	return sb.String(), nil
}

// formatASSTime formats ASS time "H:MM:SS.CC" into VTT "HH:MM:SS.CCC".
func formatASSTime(assTime string) string {
	parts := strings.Split(assTime, ":")
	if len(parts) < 3 {
		return "00:00:00.000"
	}
	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])

	secParts := strings.Split(parts[2], ".")
	secs, _ := strconv.Atoi(secParts[0])
	var millis int
	if len(secParts) > 1 {
		centis := secParts[1]
		if len(centis) == 2 {
			c, _ := strconv.Atoi(centis)
			millis = c * 10
		} else if len(centis) == 3 {
			m, _ := strconv.Atoi(centis)
			millis = m
		}
	}

	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, millis)
}

// ApplyTimeOffset shifts all timestamps in a WebVTT content string by offsetMs milliseconds.
func ApplyTimeOffset(vtt string, offsetMs int) string {
	if offsetMs == 0 {
		return vtt
	}

	return vttTimeRegex.ReplaceAllStringFunc(vtt, func(match string) string {
		sub := vttTimeRegex.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		startMs := parseVTTTime(sub[1])
		endMs := parseVTTTime(sub[2])

		startMs += offsetMs
		endMs += offsetMs

		if startMs < 0 {
			startMs = 0
		}
		if endMs < 0 {
			endMs = 0
		}

		trailing := ""
		if len(sub) > 3 {
			trailing = sub[3]
		}

		return fmt.Sprintf("%s --> %s%s", formatVTTTime(startMs), formatVTTTime(endMs), trailing)
	})
}

func parseVTTTime(t string) int {
	parts := strings.Split(t, ":")
	if len(parts) < 3 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])

	secParts := strings.Split(parts[2], ".")
	s, _ := strconv.Atoi(secParts[0])
	ms := 0
	if len(secParts) > 1 {
		ms, _ = strconv.Atoi(secParts[1])
	}

	return h*3600000 + m*60000 + s*1000 + ms
}

func formatVTTTime(ms int) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3600000
	ms %= 3600000
	m := ms / 60000
	ms %= 60000
	s := ms / 1000
	millis := ms % 1000

	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, millis)
}
