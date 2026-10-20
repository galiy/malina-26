package web

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteSeriesJSONRawFrom(t *testing.T) {
	var b bytes.Buffer
	writeSeriesJSON(&b, map[string][]seriesPoint{"1": {{T: 100, V: 1}}}, 0, 200, 150)
	s := b.String()
	if !strings.Contains(s, `"raw_from":150`) {
		t.Fatalf("нет raw_from: %s", s)
	}
	writeSeriesJSON(&b, nil, 0, 200, 0)
	if !strings.Contains(b.String(), `"raw_from":0`) {
		t.Fatalf("raw_from=0 потерян: %s", b.String())
	}
}
