package subtitle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestXunleiSearchParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": [
				{
					"name": "SSIS-589.chs.srt",
					"url": "http://example.com/ssis589.srt",
					"ext": "srt",
					"languages": ["zh-CN"]
				},
				{
					"name": "SSIS-589.uncensored.cht.srt",
					"url": "http://example.com/ssis589-uncensored.srt",
					"ext": "srt",
					"languages": ["zh-TW"]
				}
			]
		}`))
	}))
	defer ts.Close()

	provider := NewXunleiProvider(nil)
	provider.endpoint = ts.URL

	results, err := provider.Search(context.Background(), "SSIS-589")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].DisplayName != "简体中文" {
		t.Errorf("expected '简体中文', got %q", results[0].DisplayName)
	}
	if results[1].DisplayName != "繁体中文（无码版）" {
		t.Errorf("expected '繁体中文（无码版）', got %q", results[1].DisplayName)
	}
}

func TestSubtitleCatHTMLParsing(t *testing.T) {
	searchHTML := `
	<table class="sub-table">
		<tbody>
			<tr>
				<td><a href="/subs/123/movie.html">SSIS-589 Subtitle</a></td>
			</tr>
		</tbody>
	</table>`

	entries := parseSubtitleCatSearch([]byte(searchHTML), "https://www.subtitlecat.com")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "SSIS-589 Subtitle" {
		t.Errorf("entry name = %q", entries[0].Name)
	}

	detailHTML := `
	<div class="sub-single">
		<img class="flag" alt="zh-CN" />
		<a class="green-link" href="https://www.subtitlecat.com/download/123.srt">Download</a>
	</div>
	<div class="sub-single">
		<img class="flag" alt="zh-TW" />
		<a class="green-link" href="/download/124.srt">Download</a>
	</div>
	<div class="sub-single">
		<img class="flag" alt="en" />
		<a class="green-link" href="/download/125.srt">Download</a>
	</div>`

	items := parseSubtitleCatDetail([]byte(detailHTML), "SSIS-589 Subtitle", "https://www.subtitlecat.com")
	if len(items) != 2 {
		t.Fatalf("expected 2 Chinese items (simplified and traditional), got %d", len(items))
	}

	if items[0].Language != LangSimplifiedChinese || items[0].DisplayName != "简体中文" {
		t.Errorf("first item: %+v", items[0])
	}
	if items[1].Language != LangTraditionalChinese || items[1].DisplayName != "繁体中文" {
		t.Errorf("second item: %+v", items[1])
	}
}

func TestScoreAndRankCandidates(t *testing.T) {
	candidates := []Candidate{
		{
			Name:     "OTHER-123.chs.srt",
			Ext:      "srt",
			Language: LangSimplifiedChinese,
			Version:  VersionStandard,
		},
		{
			Name:     "SSIS-589.uncensored.chs.srt",
			Ext:      "srt",
			Language: LangSimplifiedChinese,
			Version:  VersionUncensored,
		},
		{
			Name:     "SSIS-589.chs.srt",
			Ext:      "srt",
			Language: LangSimplifiedChinese,
			Version:  VersionStandard,
		},
	}

	t.Run("uncensored target video ranks uncensored subtitle first", func(t *testing.T) {
		ranked := RankCandidates(candidates, "SSIS-589", true)
		if len(ranked) != 3 {
			t.Fatalf("expected 3 candidates, got %d", len(ranked))
		}
		if ranked[0].Version != VersionUncensored {
			t.Errorf("expected uncensored version first for uncensored video, got %q (score: %d)", ranked[0].Version, ranked[0].Score)
		}
		if !strings.Contains(ranked[0].DisplayName, "无码版") {
			t.Errorf("expected uncensored label, got %q", ranked[0].DisplayName)
		}
	})

	t.Run("standard target video ranks standard subtitle first", func(t *testing.T) {
		ranked := RankCandidates(candidates, "SSIS-589", false)
		if len(ranked) != 3 {
			t.Fatalf("expected 3 candidates, got %d", len(ranked))
		}
		if ranked[0].Version != VersionStandard {
			t.Errorf("expected standard version first for standard video, got %q (score: %d)", ranked[0].Version, ranked[0].Score)
		}
		if ranked[0].DisplayName != "简体中文" {
			t.Errorf("expected '简体中文', got %q", ranked[0].DisplayName)
		}
	})
}
