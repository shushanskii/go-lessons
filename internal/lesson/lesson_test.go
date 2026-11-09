package lesson

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shushanskii/go-lessons/internal/runner"
)

// lessonsFS читает уроки с диска. В приложении их вшивает go:embed, здесь мы
// читаем тот же каталог напрямую.
func lessonsRegistry(t *testing.T) *Registry {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "lessons"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := Load(os.DirFS(root))
	if err != nil {
		t.Fatalf("не смог прочитать уроки: %v", err)
	}
	return reg
}

func TestLoadReadsAllLessons(t *testing.T) {
	reg := lessonsRegistry(t)
	if len(reg.All) < 3 {
		t.Fatalf("уроков %d, ожидалось минимум 3", len(reg.All))
	}

	seen := map[string]bool{}
	for _, l := range reg.All {
		if seen[l.ID] {
			t.Errorf("двойной id %s", l.ID)
		}
		seen[l.ID] = true
		if l.Title == "" {
			t.Errorf("урок %s без title", l.ID)
		}
		if len(l.Theory) < 200 {
			t.Errorf("урок %s: теории %d байт, похоже на заготовку", l.ID, len(l.Theory))
		}
		if c := l.ID[0]; c < '0' || c > '9' {
			t.Errorf("урок %s: имя каталога должно начинаться с цифры", l.ID)
		}
	}
}

// Форма урока проверяется на загрузке: урок без tasks.go или без проверок
// бесполезен, и узнать об этом надо сразу, а не когда студент упрётся в пустоту.
func TestLoadRejectsMalformedLessons(t *testing.T) {
	full := map[string]string{
		"01-ok/meta.txt":             "title: Норм\n",
		"01-ok/lesson.md":            "# Есть\n",
		"01-ok/tasks.go":             "package ok\n",
		"01-ok/tasks_test.go":        "package ok\n",
		"02-notests/meta.txt":        "title: Без проверок\n",
		"02-notests/lesson.md":       "# Есть\n",
		"02-notests/tasks.go":        "package ok\n",
		"03-notasks/meta.txt":        "title: Без задания\n",
		"03-notasks/lesson.md":       "# Есть\n",
		"03-notasks/tasks_test.go":   "package ok\n",
		"04-nometa/lesson.md":        "# Есть\n",
		"04-nometa/tasks.go":         "package ok\n",
		"04-nometa/tasks_test.go":    "package ok\n",
		"05-oldfields/meta.txt":      "title: Отстаревшее\nkind: snippet\n",
		"05-oldfields/lesson.md":     "# Есть\n",
		"05-oldfields/tasks.go":      "package ok\n",
		"05-oldfields/tasks_test.go": "package ok\n",
	}

	cases := []struct {
		dir  string
		want string
	}{
		{"02-notests", "tasks_test.go"},
		{"03-notasks", "tasks.go"},
		{"04-nometa", "meta.txt"},
		{"05-oldfields", "неизвестное поле"},
	}

	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for name, body := range full {
				if len(name) >= 2 && name[:2] == tc.dir[:2] {
					fsys[name] = &fstest.MapFile{Data: []byte(body)}
				}
			}
			_, err := Load(fsys)
			if err == nil {
				t.Fatalf("урок %s прошёл загрузку, хотя должен был отвалиться", tc.dir)
			}
			if !contains(err.Error(), tc.want) {
				t.Errorf("ошибка %q, а ждали упоминания %q", err, tc.want)
			}
		})
	}

	// Заодно: хороший урок обязан загрузиться.
	if _, err := Load(filterFS(full, "01-")); err != nil {
		t.Errorf("нормальный урок не загрузился: %v", err)
	}
}

// Заготовка обязана компилироваться. Иначе первое нажатие r показывает
// студенту синтаксическую ошибку вместо результата тестов, и он ищет
// проблему не там.
func TestStubsCompile(t *testing.T) {
	for _, l := range lessonsRegistry(t).All {
		t.Run(l.ID, func(t *testing.T) {
			ws := t.TempDir()
			if _, err := l.Unpack(ws); err != nil {
				t.Fatalf("не разложил урок: %v", err)
			}

			rep := runner.Check(context.Background(), runner.Options{
				Dir:     l.Dir(ws),
				Timeout: 60 * time.Second,
			})
			if rep.Fatal != "" {
				t.Fatalf("проверка не состоялась: %s", rep.Fatal)
			}
			if len(rep.Steps) == 0 {
				t.Fatal("ни одного шага проверки")
			}
			if vet := rep.Steps[0]; !vet.OK {
				t.Errorf("заготовка не проходит go vet:\n%s", vet.Output)
			}
		})
	}
}

// Ключевая проверка учебника: урок должен решаться.
//
// Без этого теста очень просто написать задание, которое не проходит ни у
// кого, — обычно виноваты сами тесты.
func TestEveryLessonIsSolvable(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "solutions")

	for _, l := range lessonsRegistry(t).All {
		t.Run(l.ID, func(t *testing.T) {
			src := filepath.Join(root, l.ID)
			sols, err := os.ReadDir(src)
			if err != nil {
				t.Fatalf("нет эталонного решения в %s: %v", src, err)
			}

			ws := t.TempDir()
			if _, err := l.Unpack(ws); err != nil {
				t.Fatalf("не разложил урок: %v", err)
			}

			for _, e := range sols {
				if e.IsDir() {
					continue
				}
				b, err := os.ReadFile(filepath.Join(src, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(l.Dir(ws), e.Name()), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			rep := runner.Check(context.Background(), runner.Options{
				Dir:     l.Dir(ws),
				AlsoRun: l.Run,
				Race:    l.Race,
				Timeout: 120 * time.Second,
			})
			if rep.Fatal != "" {
				t.Fatalf("проверка не состоялась: %s", rep.Fatal)
			}
			if !rep.OK {
				t.Errorf("эталонное решение не прошло проверку\n%s", reportText(rep))
			}
			if rep.Passed == 0 {
				t.Error("урок сдан, но ни одна проверка не засчитана — парсер go test -json молчит")
			}
		})
	}
}

// Каждый урок — самостоятельный модуль: тогда VS Code открывает его как
// отдельный проект, и сломать один урок нельзя для остальных.
func TestUnpackMakesStandaloneModule(t *testing.T) {
	l := lessonsRegistry(t).All[0]
	ws := t.TempDir()

	if _, err := l.Unpack(ws); err != nil {
		t.Fatal(err)
	}

	goMod := filepath.Join(l.Dir(ws), "go.mod")
	body, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatalf("go.mod не создан: %v", err)
	}
	if !contains(string(body), "module golessons/"+l.ID) {
		t.Errorf("содержимое go.mod не то:\n%s", body)
	}
	if contains(string(body), "golessons\n") {
		t.Errorf("модуль должен быть именным, а не общим:\n%s", body)
	}
}

// Раскладка не должна стирать то, что студент уже написал.
func TestUnpackKeepsStudentWork(t *testing.T) {
	l := lessonsRegistry(t).All[0]
	ws := t.TempDir()

	if _, err := l.Unpack(ws); err != nil {
		t.Fatal(err)
	}
	tasks := filepath.Join(l.Dir(ws), FileTasks)
	if err := os.WriteFile(tasks, []byte("package changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := l.Unpack(ws)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("повторная раскладка сообщает, что создала файлы")
	}
	body, err := os.ReadFile(tasks)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(body), "package changed") {
		t.Error("повторная раскладка стёрла решение студента")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func filterFS(full map[string]string, prefix string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range full {
		if contains(name, prefix) {
			fsys[name] = &fstest.MapFile{Data: []byte(body)}
		}
	}
	return fsys
}

func reportText(r *runner.Report) string {
	var b []byte
	for _, s := range r.Steps {
		b = append(b, s.Name...)
		b = append(b, ": "...)
		b = append(b, s.Output...)
		b = append(b, '\n', '\n')
	}
	return string(b)
}
