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
	if !ready {
		return mutedStyle.Render("read/write: loading")
	}
	if !available {
		if history == nil {
			return mutedStyle.Render("read/write: no data")
		}
		return readMetricStyle.Render("read "+historyBar(history.Read)+" no data") + " / " + writeMetricStyle.Render("write "+historyBar(history.Written)+" no data")
	}
	readText := fmt.Sprintf("read %.1f rows/s", read)
	writeText := fmt.Sprintf("write %.1f rows/s", written)
	if history != nil {
		readText = fmt.Sprintf("read %s %.1f rows/s", historyBar(history.Read), read)
		writeText = fmt.Sprintf("write %s %.1f rows/s", historyBar(history.Written), written)
	}
	return readMetricStyle.Render(readText) + " / " + writeMetricStyle.Render(writeText)
}

func totalRateText(ready bool, value float64, available bool, history [5]uint64, hasHistory bool) string {
	if !ready {
		return writeMetricStyle.Render("total: loading")
	}
	text := "total: no data"
	if available {
		text = fmt.Sprintf("total: %.1f rows/s", value)
	}
	if hasHistory {
		if available {
			text = fmt.Sprintf("total: %s %.1f rows/s", historyBar(history), value)
		} else {
			text = "total: " + historyBar(history) + " no data"
		}
	}
	return writeMetricStyle.Render(text)
}
