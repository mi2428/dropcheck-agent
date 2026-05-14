package render

import (
	"strings"
	"testing"
)

func TestWriteDisplayTableAlignsWideCells(t *testing.T) {
	var b strings.Builder
	writeDisplayTable(&b,
		[]displayTableColumn{
			{header: "SSID"},
			{header: "BSSID"},
		},
		[][]string{
			{"grape", "aa:bb:cc:dd:ee:ff"},
			{"たか", "11:22:33:44:55:66"},
			{"Ｇｕｅｓｔ", "22:33:44:55:66:77"},
		},
	)

	out := b.String()
	asciiLine := renderedLineContaining(out, "aa:bb:cc:dd:ee:ff")
	japaneseLine := renderedLineContaining(out, "11:22:33:44:55:66")
	fullWidthLine := renderedLineContaining(out, "22:33:44:55:66:77")
	if asciiLine == "" || japaneseLine == "" || fullWidthLine == "" {
		t.Fatalf("rendered table missing rows:\n%s", out)
	}
	wantColumn := displayColumn(asciiLine, "aa:bb:cc:dd:ee:ff")
	for _, tt := range []struct {
		name  string
		line  string
		bssid string
	}{
		{name: "japanese", line: japaneseLine, bssid: "11:22:33:44:55:66"},
		{name: "fullwidth", line: fullWidthLine, bssid: "22:33:44:55:66:77"},
	} {
		if got := displayColumn(tt.line, tt.bssid); got != wantColumn {
			t.Fatalf("%s BSSID column=%d, want %d\n%s", tt.name, got, wantColumn, out)
		}
	}
}

func TestDisplayWidthHandlesBasicMultibyteCases(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{value: "grape", want: 5},
		{value: "たか", want: 4},
		{value: "Ｇ", want: 2},
		{value: "한", want: 2},
		{value: "A\u0301", want: 1},
	}
	for _, tt := range tests {
		if got := displayWidth(tt.value); got != tt.want {
			t.Fatalf("displayWidth(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestFitDisplayCellTruncatesWideCellsByDisplayWidth(t *testing.T) {
	got := fitDisplayCell("日本語AP", 5)
	if got != "日..." {
		t.Fatalf("fitDisplayCell() = %q, want %q", got, "日...")
	}
	if width := displayWidth(got); width > 5 {
		t.Fatalf("fitDisplayCell() display width=%d exceeds limit: %q", width, got)
	}
}
