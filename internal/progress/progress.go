// Package progress хранит, что студент уже прошёл.
package progress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Record — состояние одного урока.
type Record struct {
	Done bool `json:"done"`
	// Attempts — сколько раз запускали проверку, а не сколько раз решали.
	Attempts int `json:"attempts"`
	// Passed и Failed — сколько проверок прошло и упало на последнем прогоне.
	// Именно это превращает «не сдан» в «не сдан, 3 из 5».
	Passed int       `json:"passed"`
	Failed int       `json:"failed"`
	LastAt time.Time `json:"last_at,omitempty"`
}

// Store — прогресс на диске. Формат — обычный JSON: его видно глазами,
// его можно закоммитить в свой репозиторий, и под него не нужен движок.
type Store struct {
	path    string
	Lessons map[string]Record `json:"lessons"`
}

// Open читает файл прогресса; отсутствующий файл — это пустой прогресс, а не ошибка.
func Open(path string) (*Store, error) {
	s := &Store{path: path, Lessons: map[string]Record{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("не прочитать %s: %w", path, err)
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, s); err != nil {
		// Повреждённый файл не должен ломать приложение: начинаем заново,
		// но оригинал сохраняем рядом, чтобы не потерять историю молча.
		backup := path + ".broken"
		_ = os.Rename(path, backup)
		return s, fmt.Errorf("файл прогресса повреждён, копия лежит в %s", backup)
	}
	s.path = path
	if s.Lessons == nil {
		s.Lessons = map[string]Record{}
	}
	return s, nil
}

// Get возвращает запись урока; для непройденного — нулевую.
func (s *Store) Get(id string) Record {
	return s.Lessons[id]
}

// Pass записывает результат проверки.
//
// Попытка засчитывается всегда, сданным урок становится только когда все
// проверки зелёные и ни один тест не упал.
func (s *Store) Pass(id string, ok bool, passed, failed int) error {
	r := s.Lessons[id]
	r.Attempts++
	r.LastAt = time.Now()
	r.Passed = passed
	r.Failed = failed
	if ok {
		r.Done = true
	}
	s.Lessons[id] = r
	return s.save()
}

// Reset снимает отметку об уроке — пригодится, когда захочешь решить заново.
func (s *Store) Reset(id string) error {
	delete(s.Lessons, id)
	return s.save()
}

// DoneCount — сколько уроков сдано.
func (s *Store) DoneCount() int {
	n := 0
	for _, r := range s.Lessons {
		if r.Done {
			n++
		}
	}
	return n
}

// save пишет атомарно: сначала во временный файл, потом переименование.
// Иначе падение посреди записи оставит повреждённый JSON.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
