// Package runner проверяет код студента: go vet, go test и, если нужно, go run.
package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Problem — одно место, на которое стоит посмотреть: файл, строка, текст.
//
// Нужен, чтобы по клавише открыть редактор прямо там, где упало.
type Problem struct {
	File string
	Line int
	Col  int
	Msg  string
}

// String — как это печатает сам Go: file.go:12: что-то пошло не так.
func (p Problem) String() string {
	return p.Location() + ": " + p.Msg
}

// Location — только место, без сообщения: его показывать в подсказке.
func (p Problem) Location() string {
	where := p.File
	if p.Line > 0 {
		where += ":" + strconv.Itoa(p.Line)
		if p.Col > 0 {
			where += ":" + strconv.Itoa(p.Col)
		}
	}
	return where
}

// TestResult — исход одной проверки из go test.
type TestResult struct {
	Name    string
	Status  string // pass, fail или skip
	Elapsed time.Duration
	Output  string
	Where   *Problem // первая строка с координатами в выводе теста
}

// Step — один шаг проверки.
type Step struct {
	Name     string
	Cmd      string
	OK       bool
	Elapsed  time.Duration
	Problems []Problem
	// Output — сырой вывод. Нужен, когда парсер не распознал формат:
	// молчать вместо ошибки было бы хуже.
	Output string
}

// Report — итог одной проверки.
type Report struct {
	Steps []Step
	Tests []TestResult

	Passed  int
	Failed  int
	Skipped int

	OK       bool
	TimedOut bool
	Elapsed  time.Duration

	// Fatal — проверка не состоялась: нет компилятора, не туда записались.
	Fatal string
}

// Options — настройки прогона.
type Options struct {
	// Dir — папка урока.
	Dir string
	// AlsoRun — дополнительно выполнить go run и показать вывод.
	AlsoRun bool
	// Race — добавить -race к тестам.
	Race bool
	// Timeout — сколько ждать одну команду. Ноль означает «сам подберу».
	Timeout time.Duration
}

// stepSpec — что показать студенту и что для этого запустить.
type stepSpec struct {
	name string
	args []string
	// json — вывод разбирается как поток событий go test -json.
	json bool
}

const defaultTimeout = 30 * time.Second

// Check последовательно прогоняет vet, тесты и (опционально) запуск.
//
// Останавливается на первом провале: показывать результаты тестов, когда код
// даже не собирается, бессмысленно — студенту нужен первый текст ошибки.
func Check(ctx context.Context, o Options) *Report {
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
		if o.Race {
			// Сборка с детектором гонок заметно дольше обычной.
			timeout = 90 * time.Second
		}
	}

	goBin := findGo()
	if goBin == "" {
		return &Report{Fatal: "не нашёл компилятор go. Установи его — brew install go — " +
			"или запусти приложение из оболочки, где go есть в PATH"}
	}
	if _, err := os.Stat(o.Dir); err != nil {
		return &Report{Fatal: fmt.Sprintf("нет папки урока: %s", o.Dir)}
	}

	testArgs := []string{"test", "-json", "-count=1", "-timeout", "15s", "."}
	if o.Race {
		testArgs = []string{"test", "-race", "-json", "-count=1", "-timeout", "60s", "."}
	}

	steps := []stepSpec{
		{"разбор", []string{"vet", "."}, false},
		{"тесты", testArgs, true},
	}
	if o.AlsoRun {
		steps = append(steps, stepSpec{"запуск", []string{"run", "."}, false})
	}

	rep := &Report{OK: true}
	start := time.Now()

	for _, spec := range steps {
		t0 := time.Now()
		out, err, timedOut := runCmd(ctx, goBin, spec.args, o.Dir, timeout)
		step := Step{
			Name:    spec.name,
			Cmd:     "go " + strings.Join(spec.args, " "),
			OK:      err == nil,
			Elapsed: time.Since(t0),
			Output:  strings.TrimRight(out, "\n"),
		}

		if spec.json {
			applyTestJSON(&step, rep, out)
		} else {
			step.Problems = parseProblems(out)
		}

		rep.Steps = append(rep.Steps, step)
		if timedOut {
			rep.TimedOut = true
		}
		if err != nil || timedOut || !step.OK {
			rep.OK = false
			break
		}
	}

	rep.Elapsed = time.Since(start)
	return rep
}

// applyTestJSON разбирает поток go test -json и наполняет шаг и отчёт.
//
// Разбираем, а не показываем сырой вывод по двум причинам: нужен счётчик
// «сколько проверок прошло» и координаты для перехода в редактор. Если разбор
// не удался, сырой вывод остаётся в Step.Output — молчание было бы хуже.
func applyTestJSON(step *Step, rep *Report, raw string) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	// Вывод теста приходит построчно и относится к Test из текущего события.
	pending := map[string]*TestResult{}
	var buildErr strings.Builder
	var packageOut strings.Builder

	flush := func(name string, status string, elapsed time.Duration) {
		tr := pending[name]
		if tr == nil {
			tr = &TestResult{Name: name}
		}
		tr.Status = status
		tr.Elapsed = elapsed
		if tr.Where == nil {
			tr.Where = firstProblem(tr.Output)
		}
		rep.Tests = append(rep.Tests, *tr)
		switch status {
		case "pass":
			rep.Passed++
		case "fail":
			rep.Failed++
			step.Problems = append(step.Problems, problemsOf(*tr)...)
		case "skip":
			rep.Skipped++
		}
		delete(pending, name)
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 || line[0] != '{' {
			packageOut.Write(line)
			packageOut.WriteByte('\n')
			continue
		}

		var ev struct {
			Action  string  `json:"Action"`
			Package string  `json:"Package"`
			Test    string  `json:"Test"`
			Elapsed float64 `json:"Elapsed"`
			Output  string  `json:"Output"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			packageOut.Write(line)
			packageOut.WriteByte('\n')
			continue
		}

		switch ev.Action {
		case "build-output":
			buildErr.WriteString(ev.Output)
		case "run":
			if ev.Test != "" {
				pending[ev.Test] = &TestResult{Name: ev.Test}
			}
		case "output":
			if ev.Test != "" {
				tr := pending[ev.Test]
				if tr == nil {
					tr = &TestResult{Name: ev.Test}
					pending[ev.Test] = tr
				}
				tr.Output += ev.Output
			} else {
				packageOut.WriteString(ev.Output)
			}
		case "pass", "fail", "skip":
			d := time.Duration(ev.Elapsed * float64(time.Second))
			if ev.Test != "" {
				flush(ev.Test, ev.Action, d)
			} else if ev.Action == "fail" {
				step.OK = false
			}
		}
	}

	if buildErr.Len() > 0 {
		step.OK = false
		step.Problems = append(parseProblems(buildErr.String()), step.Problems...)
		if strings.TrimSpace(step.Output) == "" {
			step.Output = strings.TrimRight(buildErr.String(), "\n")
		}
	}

	// Пакет упал без конкретного теста: паника, дедлок или гонка.
	// Тесты при этом могут быть зелёными, поэтому проверяем отдельно.
	if packageFailed(packageOut.String()) {
		step.OK = false
	}

	if !step.OK && len(step.Problems) == 0 {
		step.Problems = parseProblems(packageOut.String())
	}
}

func packageFailed(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "FAIL") || strings.HasPrefix(t, "panic:") ||
			strings.Contains(t, "DATA RACE") {
			return true
		}
	}
	return false
}

func problemsOf(tr TestResult) []Problem {
	if tr.Where != nil {
		return []Problem{*tr.Where}
	}
	return parseProblems(tr.Output)
}

// problemRe ловит то, что Go печатает для ошибок компиляции, vet и t.Errorf:
// файл:строка:сообщение.
//
// Вариантов записи несколько: ./a.go:3:11: у компилятора, a_test.go:12: у
// теста и vet: ./a.go:3:11: у go vet — префикс «что-то: » съедал совпадение,
// поэтому он здесь разрешён.
var problemRe = regexp.MustCompile(`^\s*(?:[\w/# ]{0,12}:\s*)?(?:\./)?([\w./\-]+\.go):(\d+):(?:(\d+):)?\s*(.+)$`)

func firstProblem(out string) *Problem {
	ps := parseProblems(out)
	if len(ps) == 0 {
		return nil
	}
	return &ps[0]
}

// parseProblems вытаскивает координаты из текста ошибки.
func parseProblems(out string) []Problem {
	var out2 []Problem
	for _, line := range strings.Split(out, "\n") {
		m := problemRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNo, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		msg := strings.TrimSpace(m[4])
		if msg == "" {
			continue
		}
		out2 = append(out2, Problem{File: m[1], Line: lineNo, Col: col, Msg: msg})
	}
	return out2
}

// runCmd запускает одну команду и возвращает её объединённый вывод.
//
// Процесс уходит в собственную группу (see setProcGroup), потому что
// бесконечный цикл в коде студента убивается только вместе со всей группой:
// одного родителя недостаточно, а go test сам поднимает тестовый бинарь.
func runCmd(ctx context.Context, goBin string, args []string, dir string, timeout time.Duration) (string, error, bool) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, goBin, args...)
	cmd.Dir = dir
	cmd.Env = env()
	setProcGroup(cmd)
	// Если процесс не среагировал на отмену контекста, убиваем группу и
	// перестаём ждать через секунду.
	cmd.Cancel = func() error { return killProcGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	timedOut := errors.Is(cctx.Err(), context.DeadlineExceeded)

	out := buf.String()
	if timedOut {
		out += fmt.Sprintf("\n[проверка прервана: прошло %s, код не завершился — похоже на бесконечный цикл]", timeout)
		return out, err, true
	}
	return out, err, false
}

// env — окружение запуска. Флаги пользователя намеренно сброшены: GOFLAGS из
// ~/.config может незаметно добавить -race или -mod=vendor и сломать проверку.
func env() []string {
	e := os.Environ()
	out := make([]string, 0, len(e)+2)
	for _, kv := range e {
		if strings.HasPrefix(kv, "GOFLAGS=") || strings.HasPrefix(kv, "GO111MODULE=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GO111MODULE=on", "GOFLAGS=")
}

// findGo ищет компилятор: сначала в PATH, потом в типовых местах установки.
// Из GUI-запуска PATH бывает урезан, и тогда go есть, но не находится.
func findGo() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	home := os.Getenv("HOME")
	candidates := []string{
		"/opt/homebrew/bin/go", // Apple Silicon, Homebrew
		"/usr/local/bin/go",    // Intel, Homebrew
		"/usr/local/go/bin/go", // официальный установщик
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, "go/bin/go"))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}
