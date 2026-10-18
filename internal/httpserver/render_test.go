package httpserver

import (
	"strings"
	"testing"
)

// Regression test: every vendored Lucide icon ships its own
// class="lucide lucide-x" attribute, so applyClass must merge into it
// instead of inserting a second class="..." - a duplicate attribute that
// a parser silently resolves by keeping only the first one, dropping
// Lucide's class and leaving the SVG markup invalid either way.
func TestApplyClassMergesIntoExistingClassAttribute(t *testing.T) {
	svg := `<svg class="lucide lucide-bug" xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`
	out := string(applyClass(svg, "icon"))

	if strings.Count(out, `class="`) != 1 {
		t.Fatalf("expected exactly one class attribute, got: %s", out)
	}
	if !strings.Contains(out, "icon") || !strings.Contains(out, "lucide-bug") {
		t.Fatalf("expected both classes merged into one attribute, got: %s", out)
	}
}

func TestApplyClassInsertsClassWhenSVGHasNone(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`
	out := string(applyClass(svg, "icon"))

	if strings.Count(out, `class="`) != 1 {
		t.Fatalf("expected exactly one class attribute, got: %s", out)
	}
	if !strings.Contains(out, `class="icon"`) {
		t.Fatalf("expected class=\"icon\" to be inserted, got: %s", out)
	}
}

// Every vendored icon file actually used by a template must merge cleanly,
// not just a synthetic example.
func TestIconFuncProducesNoDuplicateClassForEveryUsedIcon(t *testing.T) {
	used := []string{
		"bug", "chevron-left", "chevron-right", "circle-check", "circle-x",
		"copy", "filter", "folder", "layout-dashboard", "log-out", "menu",
		"refresh-cw", "settings", "sun", "trash-2", "triangle-alert",
	}
	for _, name := range used {
		html := string(iconFunc(name, "icon"))
		if html == "" {
			t.Errorf("icon %q failed to load", name)
			continue
		}
		if strings.Count(html, `class="`) != 1 {
			t.Errorf("icon %q has %d class attributes, want 1: %s", name, strings.Count(html, `class="`), html)
		}
	}
}
