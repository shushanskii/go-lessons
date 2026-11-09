// Package lesson описывает урок и достаёт его из вшитого в бинарник каталога.
package lesson

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Имена файлов внутри урока. Суффикс _test.go — не соглашение стиля, а
// механизм: go test берёт только файлы с таким окончанием.
const (
	FileTheory = "lesson.md"
	FileTasks  = "tasks.go"
	FileTests  = "tasks_test.go"
	fileMeta   = "meta.txt"
)

// Урок — это теория, задание и набор файлов для проверки.
type Lesson struct {
	ID    string // имя каталога, например 01-basics
	Title string
	// Run — после тестов дополнительно выполнить go run и показать вывод.
	Run bool
	// Race — гонять тесты с -race. Для уроков про горутины это не роскошь:
	// без детектора гонок тест проходит и на неверном коде.
	Race bool
	// Theory — текст урока в markdown.
	Theory string
	// Files — все файлы урока, включая tasks.go и tasks_test.go.
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
		return nil, fmt.Errorf("в уроках нет ни одного каталога")
	}
	return reg, nil
}

func readOne(fsys fs.FS, id string) (*Lesson, error) {
	l := &Lesson{ID: id, Files: map[string][]byte{}}

	meta, err := fs.ReadFile(fsys, filepath.Join(id, fileMeta))
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
		case "run":
			l.Run = val == "yes" || val == "true"
		case "race":
			l.Race = val == "yes" || val == "true"
		default:
			return nil, fmt.Errorf("неизвестное поле %q в %s", key, fileMeta)
		}
	}
	if l.Title == "" {
		return nil, fmt.Errorf("в %s нет title", fileMeta)
	}

	theory, err := fs.ReadFile(fsys, filepath.Join(id, FileTheory))
	if err != nil {
		return nil, fmt.Errorf("нет %s: %w", FileTheory, err)
	}
	l.Theory = string(theory)

	err = fs.WalkDir(fsys, id, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Name() == fileMeta || d.Name() == FileTheory {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		l.Files[d.Name()] = b
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Форма урока жёсткая: один вид уроков дешевле, чем два с развилками
	// в интерфейсе и в проверках.
	if _, ok := l.Files[FileTasks]; !ok {
		return nil, fmt.Errorf("нет %s — файла, который правит студент", FileTasks)
	}
	if _, ok := l.Files[FileTests]; !ok {
		return nil, fmt.Errorf("нет %s — нечем проверять результат", FileTests)
	}
	return l, nil
}

// Dir — папка урока в рабочей директории.
//
// Уроки лежат в подкаталоге lessons/, чтобы рабочая папка оставалась
// опрятной: рядом только .progress.json.
func (l *Lesson) Dir(workspace string) string {
	return filepath.Join(workspace, "lessons", l.ID)
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

	// go.mod создаём здесь, а не храним в уроке. Причина измерена: go:embed
	// пропускает каталог, в котором лежит go.mod, — для инструмента это чужой
	// модуль. Без этого урока нельзя было бы вшить в бинарник.
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); os.IsNotExist(err) {
		body := fmt.Sprintf("module golessons/%s\n\ngo 1.22\n", l.ID)
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0o644); err != nil {
			return created, fmt.Errorf("не создать go.mod: %w", err)
		}
		created = true
	}

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

// Read — содержимое файла урока из рабочей папки.
func (l *Lesson) Read(workspace, name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(l.Dir(workspace), name))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
