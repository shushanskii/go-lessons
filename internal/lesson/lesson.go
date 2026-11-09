// Package lesson описывает урок и достаёт его из вшитого в бинарник каталога.
package lesson

import (
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kind определяет, как студент пишет код.
type Kind string

const (
	// KindSnippet — маленькое поле ввода прямо в интерфейсе.
	KindSnippet Kind = "snippet"
	// KindProject — файлы в рабочей папке, правятся внешним редактором.
	KindProject Kind = "project"
)

// Файлы со служебными именами не попадают в рабочую папку студента.
const (
	fileMeta   = "meta.txt"
	fileTheory = "lesson.md"

	// EditableFile — файл, который редактирует поле ввода в режиме snippet.
	EditableFile = "solution.go"
)

// Урок — это теория, задание и набор файлов для проверки.
type Lesson struct {
	ID    string // имя каталога, например 01-variables
	Title string
	Kind  Kind
	// Run — после тестов дополнительно выполнить go run и показать вывод.
	Run bool
	// Race — гонять тесты с -race. Для уроков про горутины это не роскошь:
	// без детектора гонок тест проходит и на неверном коде.
	Race bool
	// Hint — подсказка, что открыть в редакторе (для KindProject).
	Hint string
	// Theory — текст урока в markdown.
	Theory string
	// Files — остальные файлы урока: заготовка и тесты.
	Files map[string][]byte
}

// Registry — упорядоченный список уроков.
type Registry struct {
	All []*Lesson
}

// Load читает уроки из fsys, где каждый урок лежит в каталоге <id>/.
//
// Порядок задаётся именами каталогов, поэтому числовая приставка обязательна:
// 01-..., 02-... — так порядок не зависит от файловой системы.
func Load(fsys fs.FS) (*Registry, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("не нашёл каталог с уроками: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	reg := &Registry{}
	for _, name := range names {
		l, err := readOne(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("урок %s: %w", name, err)
		}
		reg.All = append(reg.All, l)
	}
	if len(reg.All) == 0 {
		return nil, fmt.Errorf("в lessons нет ни одного урока")
	}
	return reg, nil
}

func readOne(fsys fs.FS, id string) (*Lesson, error) {
	dir := id

	l := &Lesson{ID: id, Kind: KindProject, Files: map[string][]byte{}}

	meta, err := fs.ReadFile(fsys, filepath.Join(dir, fileMeta))
	if err != nil {
		return nil, fmt.Errorf("нет %s: %w", fileMeta, err)
	}
	for _, line := range strings.Split(string(meta), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("в %s строка без двоеточия: %q", fileMeta, line)
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "title":
			l.Title = val
		case "kind":
			switch Kind(val) {
			case KindSnippet, KindProject:
				l.Kind = Kind(val)
			default:
				return nil, fmt.Errorf("неизвестный kind %q, бывает snippet или project", val)
			}
		case "run":
			l.Run = val == "yes" || val == "true"
		case "race":
			l.Race = val == "yes" || val == "true"
		case "hint":
			l.Hint = val
		default:
			return nil, fmt.Errorf("неизвестное поле %q в %s", key, fileMeta)
		}
	}
	if l.Title == "" {
		return nil, fmt.Errorf("в %s нет title", fileMeta)
	}

	theory, err := fs.ReadFile(fsys, filepath.Join(dir, fileTheory))
	if err != nil {
		return nil, fmt.Errorf("нет %s: %w", fileTheory, err)
	}
	l.Theory = string(theory)

	err = fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if name == fileMeta || name == fileTheory {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		l.Files[name] = b
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(l.Files) == 0 {
		return nil, fmt.Errorf("нет ни заготовки, ни тестов")
	}
	if l.Kind == KindSnippet {
		if _, ok := l.Files[EditableFile]; !ok {
			return nil, fmt.Errorf("режим snippet требует %s", EditableFile)
		}
		if _, ok := l.Files[EditableFile+".txt"]; ok {
			return nil, fmt.Errorf("служебный файл %s.txt не нужен", EditableFile)
		}
	}
	return l, nil
}

// Dir — папка урока внутри рабочей директории.
func (l *Lesson) Dir(workspace string) string {
	return filepath.Join(workspace, l.ID)
}

// Unpack кладёт файлы урока в рабочую папку.
//
// Существующие файлы не трогает: иначе любой запуск стирал бы решения
// студента. Возвращает true, если было создано что-то новое.
func (l *Lesson) Unpack(workspace string) (bool, error) {
	dir := l.Dir(workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}

	created := false
	for name, data := range l.Files {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue // файл уже есть, студент мог его поменять
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return created, fmt.Errorf("не записать %s: %w", path, err)
		}
		created = true
	}
	return created, nil
}

// ReadEditable — содержимое редактируемого файла для режима snippet.
func (l *Lesson) ReadEditable(workspace string) (string, error) {
	b, err := os.ReadFile(filepath.Join(l.Dir(workspace), EditableFile))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WriteEditable сохраняет поле ввода обратно в файл.
//
// Форматирует его по дороге. Поле ввода в bubbles разворачивает табы в
// четыре пробела, а gofmt живёт табами: без этого каждый сохранённый snippet
// приезжал бы в файл неотформатированным, и первая же жалоба студента
// звучала бы как «мне исправили код».
//
// Неформатируемый код (не хватает скобки) пишется как есть: студенту нужнее
// собственная опечатка, чем молча потерянное редактирование.
func (l *Lesson) WriteEditable(workspace, content string) error {
	out := content
	if formatted, err := format.Source([]byte(content)); err == nil {
		out = string(formatted)
	}
	return os.WriteFile(filepath.Join(l.Dir(workspace), EditableFile), []byte(out), 0o644)
}

// Status — короткий текст о уроке для списка.
func (l *Lesson) Status() string {
	if l.Kind == KindSnippet {
		return "поле ввода"
	}
	return "внешний редактор"
}
