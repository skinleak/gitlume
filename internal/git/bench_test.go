package git

import (
	"fmt"
	"testing"
)

// BenchmarkParseStatus parses a status with 50k changed files, roughly a
// large monorepo with an unignored dependency directory.
func BenchmarkParseStatus(b *testing.B) {
	records := []string{"# branch.oid " + oid, "# branch.head main"}
	for i := range 50_000 {
		switch i % 3 {
		case 0:
			records = append(records, fmt.Sprintf("1 .M N... 100644 100644 100644 %s %s src/pkg%d/file%d.go", hash, hash, i/100, i))
		case 1:
			records = append(records, fmt.Sprintf("1 A. N... 000000 100644 100644 %s %s src/new/file%d.go", hash, hash, i))
		default:
			records = append(records, fmt.Sprintf("? node_modules/dep%d/index.js", i))
		}
	}
	data := z(records...)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		st, err := ParseStatus(data)
		if err != nil {
			b.Fatal(err)
		}
		_ = st.Changes(Staged)
	}
}
