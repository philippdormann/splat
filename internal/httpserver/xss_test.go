package httpserver

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"errgotrack/internal/dashboard"
	"errgotrack/internal/issues"
)

// Event payloads are untrusted: an exception type, message, or tag value can
// contain anything the instrumented app's user input produced. These tests
// make sure html/template's contextual autoescaping actually neutralizes a
// script-injection attempt in every place event data reaches a page.
func TestIssueDetailEscapesUntrustedExceptionData(t *testing.T) {
	r, err := newRenderer(slog.New(slog.NewTextHandler(discardWriter{}, nil)))
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}

	payload := `<script>alert('xss')</script>`
	data := issueDetailPageData{
		PageBase: PageBase{Authenticated: true},
		Issue: &issues.Issue{
			ID: 1, Title: payload, ExceptionType: payload, ExceptionValue: payload, Status: issues.StatusUnresolved, Level: "error",
		},
		Project: &projectRow{ID: 1, Name: payload},
		Tags:    []keyValue{{Key: payload, Value: payload}},
		User:    &userView{Username: payload, Extra: []keyValue{{Key: payload, Value: payload}}},
		SDK:     &sdkView{Name: payload, Integrations: []string{payload}},
		Request: &requestView{URL: payload, QueryParams: []keyValue{{Key: payload, Value: payload}}},
	}

	rec := httptest.NewRecorder()
	r.Page(rec, "issue_detail", data)
	body := rec.Body.String()

	if strings.Contains(body, "<script>alert") {
		t.Fatalf("raw script tag leaked into rendered HTML:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected html-escaped payload in output, got:\n%s", body)
	}
}

func TestDashboardChartJSONCannotBreakOutOfScriptTag(t *testing.T) {
	// A project or exception-type name containing "</script>" must not be
	// able to terminate the <script type="application/json"> block early.
	points := []dashboard.Point{{Label: `</script><script>alert(1)</script>`, Value: 1}}
	js := string(barChart(points))
	if strings.Contains(js, "</script>") {
		t.Fatalf("chart JSON contains an unescaped script-closing sequence: %s", js)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
