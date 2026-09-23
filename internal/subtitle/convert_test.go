package subtitle

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestDecodeToUTF8(t *testing.T) {
	t.Run("utf-8 with BOM", func(t *testing.T) {
		bom := []byte{0xEF, 0xBB, 0xBF}
		raw := append(bom, []byte("你好世界")...)
		got, err := DecodeToUTF8(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got != "你好世界" {
			t.Fatalf("got %q, want %q", got, "你好世界")
		}
	})

	t.Run("GBK encoded string", func(t *testing.T) {
		gbkEncoder := simplifiedchinese.GB18030.NewEncoder()
		gbkBytes, err := gbkEncoder.Bytes([]byte("中文字幕测试"))
		if err != nil {
			t.Fatal(err)
		}

		got, err := DecodeToUTF8(gbkBytes)
		if err != nil {
			t.Fatal(err)
		}
		if got != "中文字幕测试" {
			t.Fatalf("got %q, want %q", got, "中文字幕测试")
		}
	})
}

func TestConvertToWebVTT(t *testing.T) {
	t.Run("srt to vtt conversion", func(t *testing.T) {
		srt := "1\n00:01:20,000 --> 00:01:23,500\n你好，世界！\n\n2\n00:01:25,123 --> 00:01:28,456\n第二句字幕\n"
		got, err := ConvertToWebVTT(srt, "srt")
		if err != nil {
			t.Fatal(err)
		}

		if !strings.HasPrefix(got, "WEBVTT\n\n") {
			t.Fatalf("missing WEBVTT header: %s", got)
		}
		if !strings.Contains(got, "00:01:20.000 --> 00:01:23.500") {
			t.Errorf("comma was not converted to dot: %s", got)
		}
		if !strings.Contains(got, "你好，世界！") {
			t.Errorf("text missing: %s", got)
		}
	})

	t.Run("ass to vtt conversion", func(t *testing.T) {
		ass := `[Script Info]
Title: Test
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:01:20.10,0:01:23.50,Default,,0,0,0,,{\pos(100,200)}第一句\N换行内容
Dialogue: 0,0:01:25.00,0:01:28.00,Default,,0,0,0,,第二句内容
`
		got, err := ConvertToWebVTT(ass, "ass")
		if err != nil {
			t.Fatal(err)
		}

		if !strings.HasPrefix(got, "WEBVTT\n\n") {
			t.Fatalf("missing WEBVTT header: %s", got)
		}
		if !strings.Contains(got, "00:01:20.100 --> 00:01:23.500") {
			t.Errorf("incorrect timestamp format: %s", got)
		}
		if strings.Contains(got, `{\pos`) {
			t.Errorf("ass override tags were not stripped: %s", got)
		}
		if !strings.Contains(got, "第一句\n换行内容") {
			t.Errorf("text missing or line break not formatted: %s", got)
		}
	})
}

func TestApplyTimeOffset(t *testing.T) {
	vtt := "WEBVTT\n\n1\n00:01:20.000 --> 00:01:23.500\n你好，世界！\n"

	t.Run("positive 500ms offset", func(t *testing.T) {
		shifted := ApplyTimeOffset(vtt, 500)
		if !strings.Contains(shifted, "00:01:20.500 --> 00:01:24.000") {
			t.Errorf("shifted time wrong: %s", shifted)
		}
	})

	t.Run("negative 1000ms offset", func(t *testing.T) {
		shifted := ApplyTimeOffset(vtt, -1000)
		if !strings.Contains(shifted, "00:01:19.000 --> 00:01:22.500") {
			t.Errorf("shifted time wrong: %s", shifted)
		}
	})

	t.Run("negative clamp to zero", func(t *testing.T) {
		shortVTT := "WEBVTT\n\n1\n00:00:00.200 --> 00:00:01.000\n你好\n"
		shifted := ApplyTimeOffset(shortVTT, -500)
		if !strings.Contains(shifted, "00:00:00.000 --> 00:00:00.500") {
			t.Errorf("shifted time not clamped to zero: %s", shifted)
		}
	})
}
