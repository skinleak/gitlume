package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCells(t *testing.T) {
	// Captured from lipgloss with the ANSI profile: bold+color, reset,
	// faint, reverse video (the text input cursor).
	got := parseCells("\x1b[1;94mab\x1b[0m c\x1b[2;90md\x1b[0m\x1b[7m \x1b[0m")
	want := []cell{
		{'a', style{color: 94, bold: true}},
		{'b', style{color: 94, bold: true}},
		{' ', style{}},
		{'c', style{}},
		{'d', style{color: 90, faint: true}},
		{' ', style{reverse: true}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseCells\n got: %+v\nwant: %+v", got, want)
	}
}

func TestRenderLinePositionsRuns(t *testing.T) {
	got := renderLine("  › M a<b>.go")
	for _, want := range []string{
		`<tspan x="38.86">›</tspan>`, // non-ASCII glyphs stand alone
		`<tspan x="55.72">M a&lt;b&gt;.go</tspan>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderLine missing %s:\n%s", want, got)
		}
	}
	if renderLine("   ") != "" {
		t.Error("blank line should render nothing")
	}
}

func TestRenderSVGFirstFrameVisible(t *testing.T) {
	svg := renderSVG([]frame{{"one", 1}, {"two", 1}}, "x", 10)
	if !strings.Contains(svg, `<g id="f0" class="f"><use`) || !strings.Contains(svg, `<g id="f1" class="f" opacity="0">`) {
		t.Errorf("frame visibility wrong:\n%s", svg)
	}
}
