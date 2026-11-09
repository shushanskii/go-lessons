package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/shushanskii/go-lessons/internal/lesson"
	"github.com/shushanskii/go-lessons/internal/runner"
)

// Палитра. Числа — xterm-256, чтобы не зависеть от темы терминала.
var (
	cAccent  = lipgloss.Color("39")
	cOK      = lipgloss.Color("42")
	cFail    = lipgloss.Color("203")
	cMuted   = lipgloss.Color("244")
	cText    = lipgloss.Color("252")
	cWarning = lipgloss.Color("214")

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	styleMuted = lipgloss.NewStyle().Foreground(cMuted)
	styleOK    = lipgloss.NewStyle().Foreground(cOK).Bold(true)
	styleFail  = lipgloss.NewStyle().Foreground(cFail).Bold(true)
	styleWarn  = lipgloss.NewStyle().Foreground(cWarning).Bold(true)
	styleSel   = lipgloss.NewStyle().Foreground(cAccent).Bold(true)

	styleFooter = lipgloss.NewStyle().Foreground(cMuted).Padding(0, 1)
)

func paneStyle(active bool) lipgloss.Style {
	border := lipgloss.RoundedBorder()
	col := cMuted
	if active {
		col = cAccent
	}
	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(col).
		Padding(0, 1)
}

// ---------- главный вид ----------

func (m Model) View() tea.View {
	var v tea.View

	switch m.screen {
	case screenList:
		v = tea.NewView(m.viewList())
	default:
		v = tea.NewView(m.viewLesson())
	}

	v.AltScreen = true
	if m.editing {
		v.Cursor = m.ta.Cursor()
	}
	return v
}

// ---------- список ----------

func (m Model) viewList() string {
	header := styleTitle.Render("  Go: интерактивный тренажёр") +
		styleMuted.Render(fmt.Sprintf("   сдано %d из %d", m.store.DoneCount(), len(m.reg.All))) +
		"\n  рабочая папка: " + m.workspace + "\n"

	footer := styleFooter.Render(m.notice + "\n" + "↑↓ выбор · Enter открыть · q выход")

	body := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cAccent).
		Padding(0, 1).
		Width(m.width - 4).
		Render(m.listVP.View())

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) renderList() {
	var b strings.Builder
	done := m.store.DoneCount()

	fmt.Fprintf(&b, "Программа: %s, сдано %d\n\n", lessonsWord(len(m.reg.All)), done)

	for i, l := range m.reg.All {
		rec := m.store.Get(l.ID)

		var mark string
		switch {
		case rec.Done:
			mark = styleOK.Render("✓")
		case rec.Attempts > 0:
			mark = styleWarn.Render("…")
		default:
			mark = styleMuted.Render("·")
		}

		num := styleMuted.Render(fmt.Sprintf("%2d.", i+1))
		meta := styleMuted.Render("  " + l.Status())
		if l.Run {
			meta += styleMuted.Render(" + запуск")
		}

		title := l.Title
		if i == m.idx {
			title = styleSel.Render(title)
			num = styleSel.Render(fmt.Sprintf("%2d>", i+1))
		}

		fmt.Fprintf(&b, "%s %s %s%s\n", num, mark, title, meta)
	}

	m.listVP.SetContent(b.String())
}

// ---------- урок ----------

func (m Model) viewLesson() string {
	l := m.cur
	rec := m.store.Get(l.ID)

	state := styleMuted.Render("не сдан")
	if rec.Done {
		state = styleOK.Render("сдан")
	} else if rec.Attempts > 0 {
		state = styleWarn.Render(fmt.Sprintf("попыток: %d", rec.Attempts))
	}

	status := "…"
	switch {
	case m.running:
		status = m.sp.View() + " проверяю"
	case m.report != nil && m.report.OK:
		status = styleOK.Render("готово")
	case m.report != nil:
		status = styleFail.Render("ошибки")
	}

	header := lipgloss.NewStyle().Padding(0, 1).Render(
		styleTitle.Render(fmt.Sprintf("%02d · %s", m.idx+1, l.Title)) +
			"   " + state + "   " + status,
	)

	left := paneStyle(m.right == tabCode).
		Width(m.theoryVP.Width()).
		Height(m.theoryVP.Height()).
		Render(m.theoryVP.View())

	right := paneStyle(m.right == tabResult).
		Width(m.outVP.Width()).
		Height(m.outVP.Height()).
		Render(m.viewRightPane())

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Padding(0, 0, 0, 1).Render(left),
		lipgloss.NewStyle().Padding(0, 1, 0, 0).Render(right),
	)

	notice := styleMuted.Render(" " + m.notice)

	var keys string
	switch {
	case m.editing:
		keys = "Escape — выйти из правки · r — проверить"
	case l.Kind == lesson.KindSnippet:
		keys = "r проверить · tab править код · 1 теория · 2 проверка · n следующий · esc список · q выход"
	default:
		keys = "r проверить · e редактор · 1 теория · 2 проверка · j/k прокрутка · n следующий · esc список · q выход"
	}
	footer := styleFooter.Render(keys)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, notice, footer)
}

// renderOutput наполняет прокручиваемую правую панель.
//
// Для snippet-режима на вкладке «код» панель не используется: там рисует
// себя textarea, у неё собственный скролл и курсор.
func (m *Model) renderOutput() {
	if m.cur == nil || m.width == 0 {
		return
	}

	var content string
	switch {
	case m.right == tabResult:
		content = m.resultText()
	case m.cur.Kind == lesson.KindProject:
		content = m.projectFilesText()
	default:
		return
	}
	m.outVP.SetContent(wrapTo(content, m.outVP.Width()))
	m.outVP.GotoTop()
}

func (m Model) viewRightPane() string {
	if m.right == tabCode && m.cur.Kind == lesson.KindSnippet {
		return m.viewEditor()
	}
	return m.outVP.View()
}

// viewEditor — поле ввода для snippet-урока.
func (m Model) viewEditor() string {
	if !m.editing {
		return styleMuted.Render("(tab — править)\n\n") + m.ta.View()
	}
	return m.ta.View()
}

// projectFilesText — содержимое папки урока для чтения, когда код правят снаружи.
func (m Model) projectFilesText() string {
	var b strings.Builder
	b.WriteString(styleMuted.Render("Файлы урока. Правь их в редакторе: e\n"))
	b.WriteString(styleMuted.Render(m.cur.Dir(m.workspace)) + "\n\n")

	names, err := goFiles(m.cur.Dir(m.workspace))
	if err != nil {
		return "не смог прочитать папку урока:\n" + err.Error()
	}
	for _, name := range names {
		b.WriteString(styleTitle.Render("── "+name+" ──") + "\n")
		b.WriteString(readSnippet(filepath.Join(m.cur.Dir(m.workspace), name)))
		b.WriteString("\n\n")
	}
	return b.String()
}

// resultText — вывод последней проверки.
func (m Model) resultText() string {
	if m.running {
		return m.sp.View() + " собираю и запускаю проверки…\n\n" +
			styleMuted.Render("обычно это меньше секунды")
	}
	rep := m.report
	if rep == nil {
		var b strings.Builder
		b.WriteString(styleMuted.Render("Проверку ещё не запускали.\n\n"))
		b.WriteString("Напиши код и нажми ")
		b.WriteString(styleTitle.Render("r"))
		b.WriteString(".\n\n")
		b.WriteString(styleMuted.Render("Проверка запускает go vet и go test в папке урока."))
		return b.String()
	}

	var b strings.Builder
	if rep.Fatal != "" {
		b.WriteString(styleFail.Render("Проверка не состоялась\n\n"))
		b.WriteString(rep.Fatal + "\n")
		return b.String()
	}

	if rep.OK {
		b.WriteString(styleOK.Render("✓ Все проверки прошли") +
			styleMuted.Render(fmt.Sprintf("  за %s", rep.Elapsed.Round(10*time.Millisecond))) + "\n\n")
	} else {
		b.WriteString(styleFail.Render("✗ Проверки красные") + "\n\n")
	}

	for _, s := range rep.Steps {
		mark := styleFail.Render("✗")
		if s.OK {
			mark = styleOK.Render("✓")
		}
		fmt.Fprintf(&b, "%s %-9s %s\n", mark, s.Name,
			styleMuted.Render(s.Elapsed.Round(10*time.Millisecond).String()))
		if !s.OK && strings.TrimSpace(s.Output) != "" {
			b.WriteString(indent(s.Output, "   ") + "\n")
		}
	}

	// Успешный go run интересен именно выводом, его показываем всегда.
	for _, s := range rep.Steps {
		if s.Name == "запуск" && s.OK {
			b.WriteString("\n" + styleTitle.Render("вывод программы") + "\n")
			b.WriteString(indent(s.Output, "   ") + "\n")
		}
	}

	return b.String()
}

// ---------- мелочи ----------

func goFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			names = append(names, e.Name())
		}
	}
	// solution.go первым: это то, что студент пишет, остальное — тесты.
	sort.Slice(names, func(i, j int) bool {
		if names[i] == lesson.EditableFile {
			return true
		}
		if names[j] == lesson.EditableFile {
			return false
		}
		return names[i] < names[j]
	})
	return names, nil
}

func readSnippet(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return styleMuted.Render("<не прочитать: " + err.Error() + ">")
	}
	return string(b)
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

var _ = runner.Options{} // пакет используется в Update, здесь — только типы отчёта
