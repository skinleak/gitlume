package ui

type rowKind int

const (
	rowBlank rowKind = iota
	rowHeader
	rowItem
)

// listRow is one line of a sectionList. A header's index is the first item
// of its section and count is the section's size; an item's index points
// into items.
type listRow struct {
	kind  rowKind
	index int
	count int
}

// sectionList is a cursor over items shown in sections: a header for each
// run of items in the same section, runs separated by a blank row. rows
// and rowOf are built once in setItems, so moving and rendering never
// rebuild them; lists with tens of thousands of items stay cheap. cursor
// indexes items, offset is the first visible row.
type sectionList[T any] struct {
	items  []T
	rows   []listRow
	rowOf  []int // rowOf[i] is the row of items[i]
	cursor int
	offset int
	height int
}

// setItems replaces the items. sameSection reports whether two adjacent
// items belong to the same section. The caller sets the cursor afterwards.
func (l *sectionList[T]) setItems(items []T, sameSection func(a, b T) bool) {
	l.items = items
	l.rows = make([]listRow, 0, len(items)+6)
	l.rowOf = make([]int, len(items))
	for i := 0; i < len(items); {
		j := i + 1
		for j < len(items) && sameSection(items[i], items[j]) {
			j++
		}
		if len(l.rows) > 0 {
			l.rows = append(l.rows, listRow{kind: rowBlank})
		}
		l.rows = append(l.rows, listRow{kind: rowHeader, index: i, count: j - i})
		for k := i; k < j; k++ {
			l.rowOf[k] = len(l.rows)
			l.rows = append(l.rows, listRow{kind: rowItem, index: k})
		}
		i = j
	}
}

func (l *sectionList[T]) selected() (T, bool) {
	if l.cursor < 0 || l.cursor >= len(l.items) {
		var zero T
		return zero, false
	}
	return l.items[l.cursor], true
}

// setCursor moves the cursor to item i, clamped to the list.
func (l *sectionList[T]) setCursor(i int) {
	l.cursor = clamp(i, 0, len(l.items)-1)
	l.ensureVisible()
}

// move shifts the cursor by delta and reports whether it changed.
func (l *sectionList[T]) move(delta int) bool {
	next := clamp(l.cursor+delta, 0, len(l.items)-1)
	if next == l.cursor || len(l.items) == 0 {
		return false
	}
	l.cursor = next
	l.ensureVisible()
	return true
}

func (l *sectionList[T]) setHeight(h int) {
	l.height = max(1, h)
	l.ensureVisible()
}

// ensureVisible scrolls so the cursor row, and its section header when it
// is the first item of a section, are on screen.
func (l *sectionList[T]) ensureVisible() {
	if len(l.rows) == 0 {
		l.offset = 0
		return
	}
	cur := l.rowOf[l.cursor]
	top := cur
	if cur > 0 && l.rows[cur-1].kind == rowHeader {
		top = cur - 1
	}
	if top < l.offset {
		l.offset = top
	}
	if cur >= l.offset+l.height {
		l.offset = cur - l.height + 1
	}
	l.offset = clamp(l.offset, 0, max(0, len(l.rows)-l.height))
}

// visible returns the rows on screen.
func (l *sectionList[T]) visible() []listRow {
	return l.rows[l.offset:min(len(l.rows), l.offset+l.height)]
}
