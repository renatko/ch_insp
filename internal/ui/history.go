package ui

import (
	"fmt"
	"math"
	"strings"

	"chinspector/internal/clickhouse"
)

// Each chart has its own scale.
func historyBar(values [5]uint64) string {
	levels := []rune("▁▂▃▄▅▆▇█")
	var max uint64
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, v := range values {
		if v == 0 {
			b.WriteRune('·')
			continue
		}
		level := int(math.Ceil(float64(v)/float64(max)*float64(len(levels)))) - 1
		if level < 0 {
			level = 0
		}
		if level >= len(levels) {
			level = len(levels) - 1
		}
		b.WriteRune(levels[level])
	}
	return b.String()
}

func mvRateText(ready, available bool, read, written float64, history *clickhouse.ViewHistory) string {
	text := "read/write: no data"
	if !ready {
		return "read/write: loading"
	}
	if available {
		if history == nil {
			return fmt.Sprintf("read %.1f rows/s / write %.1f rows/s", read, written)
		}
		return fmt.Sprintf("read %.1f rows/s %s / write %.1f rows/s %s", read, historyBar(history.Read), written, historyBar(history.Written))
	}
	if history != nil {
		text += fmt.Sprintf(" · read %s / write %s", historyBar(history.Read), historyBar(history.Written))
	}
	return text
}

func totalRateText(ready bool, value float64, available bool, history [5]uint64, hasHistory bool) string {
	if !ready {
		return "total: loading"
	}
	text := "total: no data"
	if available {
		text = fmt.Sprintf("total: %.1f rows/s", value)
	}
	if hasHistory {
		text += fmt.Sprintf(" %s", historyBar(history))
	}
	return text
}
