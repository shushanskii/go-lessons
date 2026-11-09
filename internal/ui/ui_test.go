package ui

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/shushanskii/go-lessons/internal/lesson"
	"github.com/shushanskii/go-lessons/internal/progress"
)

func newTestModel(t *testing.T) (Model, string) {
	t.Helper()

	root := filepath.Join("..", "..", "testdata", "lessons")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := lesson.Load(os.DirFS(abs))
	if err != nil {
		t.Fatalf("не смог прочитать уроки: %v", err)
	}

	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "go.mod"),
		[]byte("module golessons\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := progress.Open(filepath.Join(ws, ".progress.json"))
	if err != nil {
		t.Fatal(err)
	}

	m := New(reg, store, ws)
	// Размер терминала приходит от настоящего терминала; в тесте задаём сами,
	// иначе вьюха пустая.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	return m, ws
}

// key — печатная клавиша. Для служебных ключей она не годится: у [tea.KeyPressMsg]
// метод String() строится из Code, но если заполнен ещё и Text, выигрывает он.
// То есть key(tea.KeyTab) даст "\t", а обработчик ждёт "tab".
func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// keyCode — служебный ключ: Enter, Tab, Escape, стрелки.
func keyCode(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c} }

// content возвращает экран без ANSI-кодов: проверять текст по нему
// одно, а разбирать escape-последовательности глазами — совсем другое.
func content(t *testing.T, m Model) string {
	t.Helper()
	v := m.View()
	if v.Content == "" {
		t.Fatal("View вернул пустой контент")
	}
	return ansi.Strip(v.Content)
}

func TestListShowsLessons(t *testing.T) {
	m, ws := newTestModel(t)
	got := content(t, m)

	for _, want := range []string{"Go: интерактивный тренажёр", "Переменные", "Срезы", "Горутины", ws} {
		if !strings.Contains(got, want) {
			t.Errorf("в списке нет %q\n----\n%s", want, got)
		}
	}
	if !strings.Contains(got, "q выход") {
		t.Error("подсказка про выход потерялась")
	}
}

func TestOpenLessonShowsTheoryAndUnpacksFiles(t *testing.T) {
	m, ws := newTestModel(t)

	updated, _ := m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)
	got := content(t, m)

	if !strings.Contains(got, "Переменные") {
		t.Errorf("похоже, не открылся первый урок\n%s", got)
	}
	if !strings.Contains(got, "r проверить") {
		t.Errorf("в подсказках нет клавиши проверки\n%s", got)
	}

	// Файлы урока обязаны появиться на диске.
	if _, err := os.Stat(filepath.Join(ws, "01-basics", "solution.go")); err != nil {
		t.Errorf("заготовка не разложена: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "01-basics", "solution_test.go")); err != nil {
		t.Errorf("тесты не разложены: %v", err)
	}
}

func TestEscReturnsToList(t *testing.T) {
	m, _ := newTestModel(t)
	updated, _ := m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)

	updated, _ = m.Update(keyCode(tea.KeyEscape))
	m = updated.(Model)

	if !strings.Contains(content(t, m), "назад к списку") {
		t.Error("Escape не вернул к списку")
	}
}

// Проверка «красным» и «зелёным» — то, ради чего всё затевалось.
func TestCheckMarksPassAndFail(t *testing.T) {
	m, ws := newTestModel(t)
	updated, _ := m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)

	// 1. Заготовка — тесты красные.
	m, cmd := runCheck(t, m)
	if cmd == nil {
		t.Fatal("r не вернул команду запуска проверки")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !strings.Contains(content(t, m), "Проверки красные") {
		t.Errorf("несделанный урок не выглядит красным\n%s", content(t, m))
	}
	if m.store.Get("01-basics").Done {
		t.Error("урок помечен сданным, хотя проверки красные")
	}

	// 2. Эталонное решение — зелёное, и прогресс записан.
	//
	// Для snippet-урока источник правды — поле ввода: перед проверкой оно
	// перетирает файл, поэтому правим и его, а не только файл на диске.
	sol, err := os.ReadFile(filepath.Join("..", "..", "testdata", "solutions", "01-basics", "solution.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "01-basics", "solution.go"), sol, 0o644); err != nil {
		t.Fatal(err)
	}
	m.reloadEditable()

	m, cmd = runCheck(t, m)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	if !strings.Contains(content(t, m), "Все проверки прошли") {
		t.Errorf("сделанный урок не выглядит зелёным\n%s", content(t, m))
	}
	if !m.store.Get("01-basics").Done {
		t.Error("прогресс не записан")
	}
	if _, err := os.Stat(filepath.Join(ws, ".progress.json")); err != nil {
		t.Errorf("файл прогресса не появился: %v", err)
	}
}

// Сохранение из поля ввода обязано отдавать gofmt: поле ввода печатает
// отступы пробелами, а Go живёт табами.
func TestSaveFromEditorIsGofmtClean(t *testing.T) {
	m, ws := newTestModel(t)
	updated, _ := m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)

	// Код с пробельными отступами — ровно то, что отдаёт поле ввода.
	m.ta.SetValue("package basics\n\nfunc Greeting(name string) string {\n    return \"Привет, \" + name + \"!\"\n}\n\nfunc Sum(a, b int) int {\n    return a + b\n}\n\nfunc WordCount(s string) int { return 1 }\n")

	_, cmd := m.Update(key('r'))
	if cmd == nil {
		t.Fatal("нет команды проверки")
	}

	got, err := os.ReadFile(filepath.Join(ws, "01-basics", "solution.go"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := format.Source(got)
	if err != nil {
		t.Fatalf("сохранённый код даже не парсится: %v\n%s", err, got)
	}
	if string(got) != string(want) {
		t.Errorf("код сохранён без gofmt:\n%s", got)
	}
}

func runCheck(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(key('r'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("нет команды проверки")
	}
	// Дожидаться фонового прогона здесь нечего: cmd() синхронная.
	return m, cmd
}

// Поле ввода snippet-режима должно показывать код с диска и переключаться.
func TestSnippetEditorToggles(t *testing.T) {
	m, ws := newTestModel(t)
	updated, _ := m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)

	src, err := os.ReadFile(filepath.Join(ws, "01-basics", "solution.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.ta.Value(), "func Greeting") {
		t.Error("поле ввода не заполнилось заготовкой")
	}
	// Поле ввода показывает табы как четыре пробела — это его поведение,
	// а не потеря кода. Сравниваем с тем же допущением.
	inField := strings.ReplaceAll(m.ta.Value(), "\t", "    ")
	onDisk := strings.ReplaceAll(string(src), "\t", "    ")
	if strings.TrimRight(inField, "\n") != strings.TrimRight(onDisk, "\n") {
		t.Errorf("поле ввода разошлось с файлом на диске")
	}

	updated, _ = m.Update(keyCode(tea.KeyTab))
	m = updated.(Model)
	if !m.editing {
		t.Error("tab не включил режим правки")
	}

	updated, _ = m.Update(keyCode(tea.KeyEscape))
	m = updated.(Model)
	if m.editing {
		t.Error("Escape не выключил режим правки")
	}
}

// Проектный урок правится снаружи, поэтому поля ввода у него нет.
func TestProjectLessonHasNoEditor(t *testing.T) {
	m, _ := newTestModel(t)

	// Спускаемся к третьему уроку и открываем его.
	for i := 0; i < 2; i++ {
		updated, _ := m.Update(keyCode(tea.KeyDown))
		m = updated.(Model)
	}
	updated, _ := m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)

	if m.cur.Kind != lesson.KindProject {
		t.Fatalf("ожидался проектный урок, а %q", m.cur.Kind)
	}
	got := content(t, m)
	if !strings.Contains(got, "main.go") {
		t.Errorf("не показаны файлы проекта\n%s", got)
	}
	if !strings.Contains(got, "e редактор") {
		t.Error("нет подсказки про редактор")
	}
}

// Теория проходит через glamour: проверяем, что markdown становится текстом,
// а не показывается сырым.
func TestTheoryIsRendered(t *testing.T) {
	m, _ := newTestModel(t)
	updated, _ := m.Update(keyCode(tea.KeyEnter))
	m = updated.(Model)

	if !strings.Contains(m.theoryVP.GetContent(), "Переменные") {
		t.Error("теория не попала в панель")
	}
	// Сырой markdown с решётками заголовков наружу выходить не должен.
	if strings.Contains(m.theoryVP.GetContent(), "# Переменные") {
		t.Error("показываем markdown вместо отрендеренного текста")
	}
}
