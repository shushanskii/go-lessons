// Package ui — терминальный интерфейс тренажёра.
package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/shushanskii/go-lessons/internal/lesson"
	"github.com/shushanskii/go-lessons/internal/progress"
	"github.com/shushanskii/go-lessons/internal/runner"
)

type screen int

const (
	screenList screen = iota
	screenLesson
)

type rightTab int

const (
	tabCode rightTab = iota
	tabResult
)

// checkDoneMsg приходит из фонового прогона проверок.
type checkDoneMsg struct {
	rep *runner.Report
	err error
}

// editorDoneMsg приходит, когда студент закрыл внешний редактор.
type editorDoneMsg struct{ err error }

// Модель приложения.
type Model struct {
	reg       *lesson.Registry
	store     *progress.Store
	workspace string
	md        *markdown

	width, height int

	screen screen
	idx    int // курсор в списке уроков
	cur    *lesson.Lesson

	listVP   viewport.Model
	theoryVP viewport.Model
	outVP    viewport.Model
	ta       textarea.Model
	sp       spinner.Model

	right   rightTab
	editing bool
	running bool

	report *runner.Report
	notice string // короткое сообщение о последнем действии
}

// New собирает стартовую модель.
func New(reg *lesson.Registry, store *progress.Store, workspace string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ta := textarea.New()
	ta.SetWidth(60)
	ta.SetHeight(10)
	// Нумерацию строк и префикс на каждой строке выключаем: они съедают
	// колонки, а для Go отступ важнее нумерации — курсор и так показывает, где ты.
	ta.ShowLineNumbers = false
	ta.Prompt = " "
	ta.CharLimit = 0
	ta.Blur()

	m := Model{
		reg:       reg,
		store:     store,
		workspace: workspace,
		md:        newMarkdown(),
		screen:    screenList,
		ta:        ta,
		sp:        s,
		listVP:    viewport.New(),
		theoryVP:  viewport.New(),
		outVP:     viewport.New(),
		notice:    "выбери урок и нажми Enter",
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

// ---------- сообщения ----------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd

	case checkDoneMsg:
		m.running = false
		if msg.err != nil {
			m.notice = "проверка не удалась: " + msg.err.Error()
			return m, nil
		}
		m.report = msg.rep
		m.right = tabResult
		m.renderOutput()

		id := ""
		if m.cur != nil {
			id = m.cur.ID
			if err := m.store.Pass(id, msg.rep.OK); err != nil {
				m.notice = "не смог записать прогресс: " + err.Error()
			}
		}
		switch {
		case msg.rep.Fatal != "":
			m.notice = msg.rep.Fatal
		case msg.rep.OK:
			m.notice = fmt.Sprintf("готово за %s — урок сдан", msg.rep.Elapsed.Round(10*time.Millisecond))
		case msg.rep.TimedOut:
			m.notice = "код не завершился за отведённое время"
		default:
			m.notice = "проверки красные — читай первую ошибку сверху"
		}
		return m, nil

	case editorDoneMsg:
		if msg.err != nil {
			m.notice = "редактор вернул ошибку: " + msg.err.Error()
		} else {
			m.notice = "редактор закрыт, нажми r для проверки"
			m.reloadEditable()
			m.renderOutput()
		}
		return m, nil
	}

	if m.screen == screenList {
		return m.updateList(msg)
	}
	return m.updateLesson(msg)
}

// ---------- список уроков ----------

func (m Model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.idx < len(m.reg.All)-1 {
				m.idx++
			}
			m.syncListCursor()
			return m, nil
		case "k", "up":
			if m.idx > 0 {
				m.idx--
			}
			m.syncListCursor()
			return m, nil
		case "enter":
			return m.openLesson(m.reg.All[m.idx])
		}
	}

	var cmd tea.Cmd
	m.listVP, cmd = m.listVP.Update(msg)
	return m, cmd
}

// openLesson, startCheck и openEditor — со значением в приёмнике, а не с
// указателем. Иначе return m отдаёт наружу *Model, и приведение back to Model
// в вызывающем коде паникует.

func (m Model) openLesson(l *lesson.Lesson) (tea.Model, tea.Cmd) {
	created, err := l.Unpack(m.workspace)
	if err != nil {
		m.notice = "не разложил файлы урока: " + err.Error()
		return m, nil
	}

	m.cur = l
	m.screen = screenLesson
	m.right = tabCode
	m.editing = false
	m.report = nil
	m.layout()
	m.reloadEditable()
	m.setTheory()

	if created {
		m.notice = "файлы урока лежат в " + l.Dir(m.workspace)
	} else if l.Kind == lesson.KindSnippet {
		m.notice = "правь код в поле справа, потом r"
	} else {
		m.notice = "правь файлы в редакторе (e), потом r"
	}
	return m, nil
}

// ---------- урок ----------

func (m Model) updateLesson(msg tea.Msg) (tea.Model, tea.Cmd) {
	// В режиме редактирования клавиши принадлежат полю ввода,
	// кроме Escape, которое из него выводит.
	if m.editing {
		if km, ok := msg.(tea.KeyPressMsg); ok && km.String() == "esc" {
			m.editing = false
			m.ta.Blur()
			m.notice = "режим правки выключен"
			return m, nil
		}
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		return m, cmd
	}

	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "backspace":
			m.cur, m.screen, m.editing = nil, screenList, false
			m.layout()
			m.syncListCursor()
			m.notice = "назад к списку — выбери следующий урок"
			return m, nil
		case "r":
			return m.startCheck()
		case "e":
			return m.openEditor()
		case "tab":
			if m.cur.Kind == lesson.KindSnippet {
				m.editing = true
				m.right = tabCode
				return m, m.ta.Focus()
			}
			m.toggleTab()
			return m, nil
		case "1":
			m.right = tabCode
			m.renderOutput()
			return m, nil
		case "2":
			m.right = tabResult
			m.renderOutput()
			return m, nil
		case "n":
			if m.idx+1 < len(m.reg.All) {
				m.idx++
				return m.openLesson(m.reg.All[m.idx])
			}
			m.notice = "это последний урок"
			return m, nil
		case "p":
			if m.idx > 0 {
				m.idx--
				return m.openLesson(m.reg.All[m.idx])
			}
			return m, nil
		case "j", "down":
			m.scroll(1)
			return m, nil
		case "k", "up":
			m.scroll(-1)
			return m, nil
		case "pgdown", "f":
			m.pageDown()
			return m, nil
		case "pgup", "b":
			m.pageUp()
			return m, nil
		case "home", "g":
			m.theoryVP.GotoTop()
			m.outVP.GotoTop()
			return m, nil
		case "end", "G":
			m.theoryVP.GotoBottom()
			m.outVP.GotoBottom()
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch m.right {
	case tabCode:
		m.theoryVP, cmd = m.theoryVP.Update(msg)
	default:
		m.outVP, cmd = m.outVP.Update(msg)
	}
	return m, cmd
}

func (m *Model) toggleTab() {
	if m.right == tabCode {
		m.right = tabResult
	} else {
		m.right = tabCode
	}
	m.renderOutput()
}

func (m *Model) scroll(n int) {
	if m.right == tabCode {
		m.theoryVP.ScrollDown(n)
	} else {
		m.outVP.ScrollDown(n)
	}
}

func (m *Model) pageDown() {
	if m.right == tabCode {
		m.theoryVP.PageDown()
	} else {
		m.outVP.PageDown()
	}
}

func (m *Model) pageUp() {
	if m.right == tabCode {
		m.theoryVP.PageUp()
	} else {
		m.outVP.PageUp()
	}
}

// startCheck запускает проверки фоном: интерфейс не должен замирать
// на полсекунды, пока идёт сборка.
func (m Model) startCheck() (tea.Model, tea.Cmd) {
	if m.running {
		m.notice = "проверка уже идёт"
		return m, nil
	}
	if m.cur == nil {
		return m, nil
	}

	// Поле ввода — источник правды для snippet-урока: сначала сохраняем.
	if m.cur.Kind == lesson.KindSnippet {
		if err := m.cur.WriteEditable(m.workspace, m.ta.Value()); err != nil {
			m.notice = "не смог сохранить код: " + err.Error()
			return m, nil
		}
	}

	m.running = true
	m.editing = false
	m.ta.Blur()
	m.notice = "проверяю…"
	m.right = tabResult
	m.renderOutput()

	dir := m.cur.Dir(m.workspace)
	wantRun, wantRace := m.cur.Run, m.cur.Race
	return m, func() tea.Msg {
		rep := runner.Check(context.Background(), runner.Options{
			Dir:     dir,
			AlsoRun: wantRun,
			Race:    wantRace,
		})
		return checkDoneMsg{rep: rep}
	}
}

// openEditor отдаёт папку урока внешнему редактору через $EDITOR.
func (m Model) openEditor() (tea.Model, tea.Cmd) {
	if m.cur == nil {
		return m, nil
	}
	ed := os.Getenv("EDITOR")
	if ed == "" {
		ed = os.Getenv("VISUAL")
	}
	if ed == "" {
		m.notice = "переменная EDITOR не задана — скажи редактору открыться самому: " + m.cur.Dir(m.workspace)
		return m, nil
	}

	// Редактор перехватывает терминал, поэтому в него идёт сразу папка урока.
	argv := strings.Fields(ed)
	cmd := exec.Command(argv[0], append(argv[1:], m.cur.Dir(m.workspace))...)
	m.editing = false
	m.ta.Blur()
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{err: err}
	})
}

func (m *Model) reloadEditable() {
	if m.cur == nil || m.cur.Kind != lesson.KindSnippet {
		return
	}
	src, err := m.cur.ReadEditable(m.workspace)
	if err != nil {
		m.notice = "не смог прочитать код: " + err.Error()
		return
	}
	m.ta.SetValue(src)
	m.ta.CursorEnd()
}

// ---------- раскладка ----------

func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	const (
		border = 2 // рамки слева и справа
		pad    = 2 // внутренние отступы
		chrome = 2 // шапка и подвал
	)
	bodyH := max(m.height-chrome, 3)
	inner := max(bodyH-border, 1)

	if m.screen == screenList {
		m.listVP.SetWidth(m.width - border - pad)
		m.listVP.SetHeight(inner)
		m.renderList()
		return
	}

	leftW := m.width * 52 / 100
	rightW := m.width - leftW

	m.theoryVP.SetWidth(leftW - border - pad)
	m.theoryVP.SetHeight(inner)
	m.outVP.SetWidth(rightW - border - pad)
	m.outVP.SetHeight(inner)
	m.ta.SetWidth(rightW - border - pad - 2)
	m.ta.SetHeight(inner)

	m.setTheory()
	m.renderOutput()
}

func (m *Model) setTheory() {
	if m.cur == nil || m.width == 0 {
		return
	}
	rendered, err := m.md.render(m.cur.Theory, m.theoryVP.Width())
	if err != nil {
		rendered = "# не смог отрендерить урок\n\n" + err.Error()
	}
	m.theoryVP.SetContent(rendered)
	m.theoryVP.GotoTop()
}

func (m *Model) syncListCursor() {
	// Держим выделенную строку в поле видимости списка.
	off := m.idx
	if h := m.listVP.Height(); h > 0 && off >= m.listVP.YOffset()+h {
		m.listVP.SetYOffset(off - h + 1)
	}
	if off < m.listVP.YOffset() {
		m.listVP.SetYOffset(off)
	}
	m.renderList()
}
