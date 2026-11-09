package lesson

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shushanskii/go-lessons/internal/runner"
)

// lessonsFS — уроки с диска. В приложении их кладёт в бинарник go:embed,
// здесь мы читаем тот же каталог напрямую.
func lessonsFS() *Registry {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "lessons"))
	if err != nil {
		panic(err)
	}
	reg, err := Load(os.DirFS(root))
	if err != nil {
		panic(err)
	}
	return reg
}

func TestLoadOneslesAllLessons(t *testing.T) {
	reg := lessonsFS()
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
		if l.Kind != KindSnippet && l.Kind != KindProject {
			t.Errorf("урок %s: неизвестный kind %q", l.ID, l.Kind)
		}
		// Уроки идут по номерам в именах каталогов — на этом держится порядок.
		if c := l.ID[0]; c < '0' || c > '9' {
			t.Errorf("урок %s: имя каталога должно начинаться с цифры", l.ID)
		}
	}
}

// Заготовка обязана компилироваться. Иначе первое нажатие r показывает
// студенту синтаксическую ошибку вместо результата тестов, и он ищет
// проблему не там.
func TestStubsCompile(t *testing.T) {
	reg := lessonsFS()
	for _, l := range reg.All {
		t.Run(l.ID, func(t *testing.T) {
			ws := t.TempDir()
			if _, err := l.Unpack(ws); err != nil {
				t.Fatalf("не разложил урок: %v", err)
			}
			writeWorkspaceGoMod(t, ws)

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
			vet := rep.Steps[0]
			if !vet.OK {
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
	reg := lessonsFS()
	solutionsRoot := filepath.Join("..", "..", "testdata", "solutions")

	for _, l := range reg.All {
		t.Run(l.ID, func(t *testing.T) {
			src := filepath.Join(solutionsRoot, l.ID)
			sols, err := os.ReadDir(src)
			if err != nil {
				t.Fatalf("нет эталонного решения в %s: %v", src, err)
			}

			ws := t.TempDir()
			if _, err := l.Unpack(ws); err != nil {
				t.Fatalf("не разложил урок: %v", err)
			}
			writeWorkspaceGoMod(t, ws)

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
		})
	}
}

// Тесты урока нельзя менять студенту молча: если их переписать, «сдан»
// становится неправдой.
func TestEveryLessonHasTests(t *testing.T) {
	for _, l := range rangeLessons(lessonsFS()) {
		found := false
		for name := range l.Files {
			if filepath.Ext(name) == ".go" && len(name) > 8 && name[len(name)-8:] == "_test.go" {
				found = true
			}
		}
		if !found {
			t.Errorf("уроку %s нечем проверять результат: нет *_test.go", l.ID)
		}
	}
}

func rangeLessons(reg *Registry) []*Lesson { return reg.All }

func writeWorkspaceGoMod(t *testing.T, ws string) {
	t.Helper()
	// Точно так же делает приложение: рабочая папка обязана быть модулем,
	// иначе go test отвечает «go.mod file not found».
	if err := os.WriteFile(filepath.Join(ws, "go.mod"),
		[]byte("module golessons\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
