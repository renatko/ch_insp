package ui

import (
	"fmt"
	"math"
	"strings"

	"ch_insp/internal/clickhouse"
)

// Each chart has its own scale.
func historyBarScaled(values, events [5]uint64, sharedMax uint64) string {
	levels := []rune("▁▂▃▄▅▆▇█")
	var max uint64
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	if sharedMax > max {
		max = sharedMax
	}
	var b strings.Builder
	for i, v := range values {
		if events[i] == 0 {
			b.WriteRune('·')
			continue
		}
		if v == 0 {
			b.WriteRune('0')
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
	return mvRateTextScaled(ready, available, read, written, history, 0, 0)
}
func mvRateTextScaled(ready, available bool, read, written float64, history *clickhouse.ViewHistory, readMax, writeMax uint64) string {
	if !ready {
		return mutedStyle.Render("read/write: loading")
	}
	if !available {
		if history == nil {
			return mutedStyle.Render("read/write: no data")
		}
		return readMetricStyle.Render("read "+historyBarScaled(history.Read, history.Events, readMax)+" no data") + " / " + writeMetricStyle.Render("write "+historyBarScaled(history.Written, history.Events, writeMax)+" no data")
	}
	readText := fmt.Sprintf("read %.1f rows/s", read)
	writeText := fmt.Sprintf("write %.1f rows/s", written)
	if history != nil {
		readText = fmt.Sprintf("read %s %.1f rows/s", historyBarScaled(history.Read, history.Events, readMax), read)
		writeText = fmt.Sprintf("write %s %.1f rows/s", historyBarScaled(history.Written, history.Events, writeMax), written)
	}
	return readMetricStyle.Render(readText) + " / " + writeMetricStyle.Render(writeText)
}

func totalRateText(ready bool, value float64, available bool, history clickhouse.TableHistory, hasHistory bool) string {
	return totalRateTextScaled(ready, value, available, history, hasHistory, 0)
}
func totalRateTextScaled(ready bool, value float64, available bool, history clickhouse.TableHistory, hasHistory bool, sharedMax uint64) string {
	if !ready {
		return writeMetricStyle.Render("total: loading")
	}
	text := "total: no data"
	if available {
		text = fmt.Sprintf("total: %.1f rows/s", value)
	}
	if hasHistory {
		if available {
			text = fmt.Sprintf("total: %s %.1f rows/s", historyBarScaled(history.Rows, history.Events, sharedMax), value)
		} else {
			text = "total: " + historyBarScaled(history.Rows, history.Events, sharedMax) + " no data"
		}
	}
	return writeMetricStyle.Render(text)
}

func metricStateLabel(d clickhouse.MetricDiagnostic) string {
	switch d.State {
	case clickhouse.MetricNoEvents:
		return "no events observed"
	case clickhouse.MetricUnsupported:
		return "unsupported engine"
	case clickhouse.MetricMetadataUnavailable:
		return "metadata unavailable"
	case clickhouse.MetricLogError:
		return "log error"
	case clickhouse.MetricUnknown:
		return "unknown"
	}
	return ""
}

func chartMaxima(views []clickhouse.View, rates clickhouse.OverviewRates) (uint64, uint64, uint64) {
	var readMax, writeMax, totalMax uint64
	for _, v := range views {
		h := rates.History[v.Database+"."+v.Name]
		for _, n := range h.Read {
			if n > readMax {
				readMax = n
			}
		}
		for _, n := range h.Written {
			if n > writeMax {
				writeMax = n
			}
		}
		target := clickhouse.InferFlow(v).Target
		for _, n := range rates.TableHistory[target].Rows {
			if n > totalMax {
				totalMax = n
			}
		}
	}
	return readMax, writeMax, totalMax
}
