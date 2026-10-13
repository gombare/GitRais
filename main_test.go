package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

// The native menu is part of the Italian UI; Edit must stay for copy/paste in the WebView.
func TestAppMenuLabels(t *testing.T) {
	called := false
	m := appMenu(func() { called = true })
	var labels []string
	for _, it := range m.Items {
		labels = append(labels, it.Label)
	}
	if len(m.Items) != 3 || m.Items[2].Label != "Aiuto" {
		t.Fatalf("top-level menus = %q, want app, Edit, Aiuto", labels)
	}
	if m.Items[1].Role != menu.EditMenuRole {
		t.Fatalf("second menu must be the Edit role menu, got %+v", m.Items[1])
	}
	help := m.Items[2].SubMenu.Items
	if len(help) != 1 || help[0].Label != "Log attività" {
		t.Fatalf("help items = %+v, want one item \"Log attività\"", help)
	}
	help[0].Click(nil)
	if !called {
		t.Fatal("the Log attività item does not open the activity log")
	}
}
