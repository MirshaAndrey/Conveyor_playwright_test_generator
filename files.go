package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ─── Загрузка задач из файла ──────────────────────────────────────────────────

func loadTasksFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var tasks []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tasks = append(tasks, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("файл пустой или содержит только комментарии")
	}
	return tasks, nil
}

// ─── Slug для имени файла ─────────────────────────────────────────────────────

var cyrillicTable = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d",
	'е': "e", 'ё': "yo", 'ж': "zh", 'з': "z", 'и': "i",
	'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n",
	'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
	'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch",
	'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "",
	'э': "e", 'ю': "yu", 'я': "ya",
}

func cyrillicToLatin(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if lat, ok := cyrillicTable[r]; ok {
			b.WriteString(lat)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var stopWords = map[string]bool{
	"с": true, "на": true, "в": true, "и": true, "не": true,
	"для": true, "по": true, "из": true, "при": true, "к": true,
	"о": true, "от": true, "до": true, "за": true, "над": true,
	"под": true, "это": true, "что": true, "как": true,
	"the": true, "a": true, "an": true, "of": true, "in": true,
	"on": true, "at": true, "for": true, "with": true, "and": true,
	"or": true, "to": true, "by": true, "is": true, "it": true,
}

var slugCleanRe = regexp.MustCompile(`[^\p{L}\p{N} ]+`)

func taskToSlug(task string) string {
	clean := slugCleanRe.ReplaceAllString(strings.ToLower(task), " ")
	clean = cyrillicToLatin(clean)

	var words []string
	for _, w := range strings.Fields(clean) {
		w = strings.TrimFunc(w, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		if w == "" || stopWords[w] {
			continue
		}
		words = append(words, w)
		if len(words) == 5 {
			break
		}
	}
	if len(words) == 0 {
		return "test"
	}
	return strings.Join(words, "_")
}

// ─── JSON Cases формат ────────────────────────────────────────────────────────

// TestCase — один тест-кейс из cases.json
type TestCase struct {
	URL        string `json:"url"`         // URL страницы для автосканирования селекторов
	Section    string `json:"section"`     // группа/модуль: "auth", "cart"
	Name       string `json:"name"`        // короткое имя для трекера и slug файла
	Task       string `json:"task"`        // полный текст задачи, уходит в промпт
	PromptHint string `json:"prompt_hint"` // подсказка для модели: селекторы, URL, стек
}

// CasesFile — корневая структура cases.json
type CasesFile struct {
	Cases []TestCase `json:"cases"`
}

// loadCasesJSON читает тест-кейсы из JSON-файла.
func loadCasesJSON(path string) ([]TestCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cf CasesFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("невалидный JSON: %v", err)
	}
	if len(cf.Cases) == 0 {
		return nil, fmt.Errorf("файл не содержит тест-кейсов")
	}
	for i, tc := range cf.Cases {
		if tc.Section == "" || tc.Name == "" || tc.Task == "" {
			return nil, fmt.Errorf("кейс #%d: поля section, name, task обязательны", i+1)
		}
	}
	return cf.Cases, nil
}

// ─── Сохранение ───────────────────────────────────────────────────────────────

func saveResult(outPath, content string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, []byte(content), 0644)
}

func autoSavePath(taskText string) string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	slug := taskToSlug(taskText)
	return filepath.Join("TestCasesTS", fmt.Sprintf("%s_%s.ts", timestamp, slug))
}

// autoSavePathForCase строит путь: TestCasesTS/<section>/<timestamp>_<name_slug>.ts
func autoSavePathForCase(tc TestCase) string {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	slug := taskToSlug(tc.Name)
	return filepath.Join("TestCasesTS", tc.Section, fmt.Sprintf("%s_%s.ts", timestamp, slug))
}
