package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ─── Загрузка задач из файла ──────────────────────────────────────────────────

// loadTasksFile читает задачи из текстового файла (по одной на строку).
// Строки начинающиеся с # и пустые — игнорируются.
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
