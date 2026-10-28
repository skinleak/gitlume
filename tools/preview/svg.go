package main

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// frame is one captured screen and how long the clip shows it.
type frame struct {
	ansi string
	dur  float64 // seconds
}

// The clip needs concrete colors for gitlume's ANSI indices: a calm dark
// terminal theme.
const (
	bgColor = "#1a1b26"
	fgColor = "#c0caf5"
)

var sgrColors = map[int]string{
	90: "#6b7394", 91: "#f7768e", 92: "#9ece6a", 93: "#e0af68",
	94: "#7aa2f7", 95: "#bb9af7", 96: "#7dcfff", 97: "#c0caf5",
	30: "#1a1b26", 37: "#c0caf5",
}

// Cell geometry in pixels.
const (
	fontSize   = 14
	cellWidth  = 8.43
	lineHeight = 19
	padX       = 22
	padTop     = 44
)

var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// style is the SGR state of one cell.
type style struct {
	color       int // 0 means the default foreground
	bold, faint bool
	reverse     bool
}

type cell struct {
	r  rune
	st style
}

// parseCells splits one line of ANSI text into styled cells. Only the SGR
// codes gitlume emits are interpreted; others are ignored.
func parseCells(line string) []cell {
	var cells []cell
	var st style
	pos := 0
	for _, m := range sgrRe.FindAllStringSubmatchIndex(line, -1) {
		for _, r := range line[pos:m[0]] {
			cells = append(cells, cell{r, st})
		}
		pos = m[1]
		codes := strings.Split(line[m[2]:m[3]], ";")
		for _, c := range codes {
			n, _ := strconv.Atoi(c) // "" is 0, a reset
			switch {
			case n == 0:
				st = style{}
			case n == 1:
				st.bold = true
			case n == 2:
				st.faint = true
			case n == 22:
				st.bold, st.faint = false, false
			case n == 7:
				st.reverse = true
			case n == 27:
				st.reverse = false
			case n == 39:
				st.color = 0
			case sgrColors[n] != "":
				st.color = n
			}
		}
	}
	for _, r := range line[pos:] {
		cells = append(cells, cell{r, st})
	}
	return cells
}

func x(col int) string { return strconv.FormatFloat(padX+float64(col)*cellWidth, 'f', 2, 64) }

func attrs(st style) string {
	var b strings.Builder
	if st.color != 0 {
		fmt.Fprintf(&b, ` fill="%s"`, sgrColors[st.color])
	}
	if st.bold {
		b.WriteString(` font-weight="bold"`)
	}
	if st.faint {
		b.WriteString(` fill-opacity=".55"`)
	}
	return b.String()
}

// renderLine draws one terminal row with its baseline at y=0, or returns ""
// for a blank row. Every run of text is placed at its exact column, and
// non-ASCII glyphs get their own position, so font fallback widths can
// never shift the rest of the line.
func renderLine(line string) string {
	cells := parseCells(line)
	var rects, spans strings.Builder
	for i := 0; i < len(cells); {
		c := cells[i]
		if c.st.reverse { // the text input's cursor
			fmt.Fprintf(&rects, `<rect x="%s" y="-%d" width="%.2f" height="%d" fill="%s"/>`,
				x(i), fontSize, cellWidth, lineHeight-2, fgColor)
			if c.r != ' ' {
				fmt.Fprintf(&spans, `<tspan x="%s" fill="%s">%s</tspan>`, x(i), bgColor, html.EscapeString(string(c.r)))
			}
			i++
			continue
		}
		if c.r == ' ' {
			i++
			continue
		}
		j := i + 1
		if c.r < 0x80 {
			for j < len(cells) && cells[j].st == c.st && cells[j].r < 0x80 &&
				!(cells[j].r == ' ' && j+1 < len(cells) && cells[j+1].r == ' ') {
				j++
			}
		}
		var text strings.Builder
		for _, o := range cells[i:j] {
			text.WriteRune(o.r)
		}
		fmt.Fprintf(&spans, `<tspan x="%s"%s>%s</tspan>`, x(i), attrs(c.st),
			html.EscapeString(strings.TrimRight(text.String(), " ")))
		i = j
	}
	if spans.Len() == 0 && rects.Len() == 0 {
		return ""
	}
	out := rects.String()
	if spans.Len() > 0 {
		out += `<text xml:space="preserve">` + spans.String() + `</text>`
	}
	return out
}

// renderSVG builds an animated SVG from frames of a terminal cols cells
// wide; gitlume does not pad its lines to the width. Identical rows are
// defined once and reused. Colors and fonts are presentation attributes, so
// renderers without CSS still draw text, and the first frame is visible
// without animation, so viewers that don't animate show a still.
func renderSVG(frames []frame, label string, cols int) string {
	rows := 0
	var total float64
	for _, f := range frames {
		lines := strings.Split(f.ansi, "\n")
		rows = max(rows, len(lines))
		for _, l := range lines {
			cols = max(cols, len([]rune(sgrRe.ReplaceAllString(l, ""))))
		}
		total += f.dur
	}
	w := int(2*padX + float64(cols)*cellWidth + 0.5)
	h := padTop + rows*lineHeight + 12

	var defs, groups, keyframes strings.Builder
	ids := map[string]string{}
	t := 0.0
	for n, f := range frames {
		var uses strings.Builder
		for r, line := range strings.Split(f.ansi, "\n") {
			frag := renderLine(line)
			if frag == "" {
				continue
			}
			id, ok := ids[frag]
			if !ok {
				id = "l" + strconv.Itoa(len(ids))
				ids[frag] = id
				fmt.Fprintf(&defs, `<g id="%s">%s</g>`, id, frag)
			}
			fmt.Fprintf(&uses, `<use xlink:href="#%s" y="%d"/>`, id, padTop+r*lineHeight+fontSize)
		}
		a, b := 100*t/total, 100*(t+f.dur)/total
		t += f.dur
		switch n {
		case 0:
			fmt.Fprintf(&keyframes, "@keyframes k0{0%%{opacity:1}%.3f%%{opacity:0}}", b)
		case len(frames) - 1:
			fmt.Fprintf(&keyframes, "@keyframes k%d{0%%{opacity:0}%.3f%%{opacity:1}}", n, a)
		default:
			fmt.Fprintf(&keyframes, "@keyframes k%d{0%%{opacity:0}%.3f%%{opacity:1}%.3f%%{opacity:0}}", n, a, b)
		}
		fmt.Fprintf(&keyframes, "#f%d{animation-name:k%d}", n, n)
		hidden := ` opacity="0"`
		if n == 0 {
			hidden = ""
		}
		fmt.Fprintf(&groups, `<g id="f%d" class="f"%s>%s</g>`, n, hidden, uses.String())
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s">`+"\n",
		w, h, w, h, html.EscapeString(label))
	fmt.Fprintf(&b, "<style>\n.f{animation-duration:%.2fs;animation-iteration-count:infinite;animation-timing-function:step-end}\n%s\n</style>\n",
		total, keyframes.String())
	fmt.Fprintf(&b, "<defs>%s</defs>\n", defs.String())
	fmt.Fprintf(&b, `<rect width="%d" height="%d" rx="10" fill="%s"/>`+"\n", w, h, bgColor)
	b.WriteString(`<circle cx="22" cy="18" r="5.5" fill="#f7768e" opacity=".8"/><circle cx="40" cy="18" r="5.5" fill="#e0af68" opacity=".8"/><circle cx="58" cy="18" r="5.5" fill="#9ece6a" opacity=".8"/>` + "\n")
	fmt.Fprintf(&b, `<g font-family="ui-monospace,SFMono-Regular,Menlo,Consolas,'DejaVu Sans Mono','Liberation Mono',monospace" font-size="%d" fill="%s">`+"\n%s\n</g>\n</svg>\n",
		fontSize, fgColor, groups.String())
	return b.String()
}
