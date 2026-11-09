package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Не тест по смыслу, а инструмент: пишет экраны в текстовые файлы, чтобы
// посмотреть их глазами или сравнить с прошлым снимком.
//
//	DUMP_DIR=/tmp/shots go test ./internal/ui -run TestDumpScreen
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

	m, _ := newTestModel(t)
	dump(t, dir, "01-list", m)

	m = update(t, m, keyCode(tea.KeyEnter)).(Model)
	dump(t, dir, "02-lesson", m)

	_, cmd := m.Update(key('r'))
	if cmd != nil {
		m = update(t, m, cmd()).(Model)
	}
	dump(t, dir, "03-lesson-failing", m)

	m = update(t, m, keyCode(tea.KeyEscape)).(Model)
	for i := 0; i < 2; i++ {
		m = update(t, m, keyCode(tea.KeyDown)).(Model)
	}
	m = update(t, m, keyCode(tea.KeyEnter)).(Model)
	m = update(t, m, key('3')).(Model)
	dump(t, dir, "04-lesson-result-pane", m)

	// Узкий терминал: проверяем, что раскладка не разваливается.
	narrow := New(m.reg, m.store, m.workspace)
	narrow = update(t, narrow, tea.WindowSizeMsg{Width: 80, Height: 24}).(Model)
	narrow = update(t, narrow, keyCode(tea.KeyEnter)).(Model)
	dump(t, dir, "05-narrow-80x24", narrow)

	t.Logf("снимки лежат в %s", dir)
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
