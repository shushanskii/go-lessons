package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Не тест по смыслу, а инструмент: пишет экраны в текстовые файлы, чтобы
// на них можно было посмотреть глазами или сравнить с прошлым снимком.
//
//	DUMP_DIR=/tmp/shots go test ./internal/ui -run DumpScreen
//
// Без DUMP_DIR пропускается.
func TestDumpScreen(t *testing.T) {
	dir := os.Getenv("DUMP_DIR")
	if dir == "" {
		t.Skip("DUMP_DIR не задан — снимки экрана не нужны")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	m, ws := newTestModel(t)
	dump(t, dir, "01-list", m)

	updated, _ := m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)
	dump(t, dir, "02-lesson-snippet", m)

	updated, _ = m.Update(keyCode(tea.KeyTab))
	m = updated.(Model)
	dump(t, dir, "03-lesson-editing", m)

	updated, _ = m.Update(keyCode(tea.KeyEscape))
	m = updated.(Model)
	_, cmd := m.Update(key('r'))
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	dump(t, dir, "04-lesson-failing", m)

	// Проектный урок с файлами.
	updated, _ = m.Update(keyCode(tea.KeyEscape))
	m = updated.(Model)
	for i := 0; i < 2; i++ {
		updated, _ = m.Update(keyCode(tea.KeyDown))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)
	dump(t, dir, "05-lesson-project", m)

	t.Logf("снимки лежат в %s (рабочая папка %s)", dir, ws)
}

func dump(t *testing.T, dir, name string, m Model) {
	t.Helper()
	// Рамки и цвета убираем: в текстовом файле важны слова и раскладка.
	text := ansi.Strip(m.View().Content)
	path := filepath.Join(dir, name+".txt")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Error(err)
	}
}
