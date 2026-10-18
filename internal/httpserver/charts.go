package httpserver

import (
	"encoding/json"
	"html/template"

	"errgotrack/internal/dashboard"
)

// chartJSON serializes a Chart.js config to a value safe to embed verbatim
// inside a <script type="application/json"> tag. encoding/json's default
// Marshal HTML-escapes '<', '>' and '&' to < etc, so the output cannot
// contain a literal "</script>" even if a label originates from untrusted
// exception text - which is why this is marked template.JS (skip
// html/template's own re-escaping) rather than passed as a plain string
// (which would JSON-quote the whole blob instead of embedding it raw).
func chartJSON(cfg map[string]any) template.JS {
	b, err := json.Marshal(cfg)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(b)
}

var palette = []string{"#4f46e5", "#dc2626", "#16a34a", "#d97706", "#0891b2", "#9333ea", "#be185d", "#65a30d"}

func lineChart(points []dashboard.Point) template.JS {
	labels := make([]string, len(points))
	values := make([]int64, len(points))
	for i, p := range points {
		labels[i], values[i] = p.Label, p.Value
	}
	return chartJSON(map[string]any{
		"type": "line",
		"data": map[string]any{
			"labels": labels,
			"datasets": []map[string]any{{
				"label": "Events", "data": values,
				"borderColor": palette[0], "backgroundColor": palette[0],
				"tension": 0.25, "pointRadius": 0, "fill": false,
			}},
		},
		"options": map[string]any{
			"plugins": map[string]any{"legend": map[string]any{"display": false}},
			"scales":  map[string]any{"y": map[string]any{"beginAtZero": true}},
		},
	})
}

func barChart(points []dashboard.Point) template.JS {
	labels := make([]string, len(points))
	values := make([]int64, len(points))
	colors := make([]string, len(points))
	for i, p := range points {
		labels[i], values[i] = p.Label, p.Value
		colors[i] = palette[i%len(palette)]
	}
	return chartJSON(map[string]any{
		"type": "bar",
		"data": map[string]any{
			"labels": labels,
			"datasets": []map[string]any{{
				"label": "Events", "data": values, "backgroundColor": colors,
			}},
		},
		"options": map[string]any{
			"plugins": map[string]any{"legend": map[string]any{"display": false}},
			"scales":  map[string]any{"y": map[string]any{"beginAtZero": true}},
		},
	})
}

func doughnutChart(points []dashboard.Point) template.JS {
	labels := make([]string, len(points))
	values := make([]int64, len(points))
	colors := make([]string, len(points))
	for i, p := range points {
		labels[i], values[i] = p.Label, p.Value
		colors[i] = palette[i%len(palette)]
	}
	return chartJSON(map[string]any{
		"type": "doughnut",
		"data": map[string]any{
			"labels":   labels,
			"datasets": []map[string]any{{"data": values, "backgroundColor": colors}},
		},
		"options": map[string]any{"plugins": map[string]any{"legend": map[string]any{"position": "right"}}},
	})
}

func newVsRecurringChart(newCount, recurringCount int64) template.JS {
	return doughnutChart([]dashboard.Point{{Label: "New", Value: newCount}, {Label: "Recurring", Value: recurringCount}})
}

// statusColors fixes a color per status (unresolved=red, resolved=green,
// ignored=gray) instead of the generic palette's position-based colors, so
// "resolved" doesn't end up red just because of its position in the list.
var statusColors = map[string]string{"unresolved": "#dc2626", "resolved": "#16a34a", "ignored": "#6b7280"}
var statusOrder = []string{"unresolved", "resolved", "ignored"}
var statusLabels = map[string]string{"unresolved": "Unresolved", "resolved": "Resolved", "ignored": "Ignored"}

// issuesByStatusChart always renders all three statuses in a fixed order
// and with fixed colors, even at zero, so the legend doesn't reshuffle or
// drop entries between page loads depending on which statuses happen to
// have activity in the selected range.
func issuesByStatusChart(points []dashboard.Point) template.JS {
	counts := make(map[string]int64, len(statusOrder))
	for _, p := range points {
		counts[p.Label] = p.Value
	}
	labels := make([]string, len(statusOrder))
	values := make([]int64, len(statusOrder))
	colors := make([]string, len(statusOrder))
	for i, status := range statusOrder {
		labels[i] = statusLabels[status]
		values[i] = counts[status]
		colors[i] = statusColors[status]
	}
	return chartJSON(map[string]any{
		"type": "doughnut",
		"data": map[string]any{
			"labels":   labels,
			"datasets": []map[string]any{{"data": values, "backgroundColor": colors}},
		},
		"options": map[string]any{"plugins": map[string]any{"legend": map[string]any{"position": "right"}}},
	})
}
