package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ─── Результат запуска теста ──────────────────────────────────────────────────

// RunResult — результат одного запуска Playwright теста.
type RunResult struct {
	Passed   bool
	Output   string        // полный stdout + stderr
	Errors   []PlaywrightError // разобранные ошибки
	Duration time.Duration
}

// PlaywrightError — одна конкретная ошибка из вывода Playwright.
type PlaywrightError struct {
	TestName string // название упавшего теста
	Message  string // текст ошибки
	Location string // файл:строка если есть
}

// ─── Запуск ───────────────────────────────────────────────────────────────────

// runPlaywrightTest запускает один .ts файл через npx playwright test.
// Возвращает RunResult с разобранными ошибками.
func runPlaywrightTest(filePath string) (RunResult, error) {
	npx, err := exec.LookPath("npx")
	if err != nil {
		return RunResult{}, fmt.Errorf("npx не найден в PATH\n[TIP] Установите Node.js: https://nodejs.org")
	}

	start := time.Now()
	cmd := exec.Command(npx, "playwright", "test", filePath, "--config=playwright.config.ts", "--reporter=line")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	runErr := cmd.Run()
	duration := time.Since(start)

	output := out.String()
	passed := runErr == nil

	return RunResult{
		Passed:   passed,
		Output:   output,
		Errors:   parsePlaywrightErrors(output),
		Duration: duration,
	}, nil
}

// ─── Парсинг ошибок ───────────────────────────────────────────────────────────

// Паттерны для разбора вывода Playwright --reporter=line
var (
	// "  1) auth › Вход в аккаунт"
	reTestName = regexp.MustCompile(`(?m)^\s+\d+\)\s+(.+)$`)
	// "Error: expect(received).toBeVisible()"
	reErrorMsg = regexp.MustCompile(`(?m)^(Error:.+|TimeoutError:.+|TypeError:.+)$`)
	// "at /path/to/file.ts:42:10"
	reLocation = regexp.MustCompile(`at\s+\S+\.ts:\d+:\d+`)
)

// parsePlaywrightErrors извлекает структурированные ошибки из вывода Playwright.
func parsePlaywrightErrors(output string) []PlaywrightError {
	var errors []PlaywrightError

	testNames := reTestName.FindAllStringSubmatch(output, -1)
	errorMsgs := reErrorMsg.FindAllString(output, -1)
	locations := reLocation.FindAllString(output, -1)

	// Соединяем по индексу — Playwright выводит ошибки последовательно
	count := len(testNames)
	for i := 0; i < count; i++ {
		e := PlaywrightError{}
		if i < len(testNames) {
			e.TestName = strings.TrimSpace(testNames[i][1])
		}
		if i < len(errorMsgs) {
			e.Message = strings.TrimSpace(errorMsgs[i])
		}
		if i < len(locations) {
			e.Location = strings.TrimSpace(locations[i])
		}
		errors = append(errors, e)
	}

	// Если паттерн не сработал — возвращаем весь output как одну ошибку
	if len(errors) == 0 && len(output) > 0 {
		errors = append(errors, PlaywrightError{
			Message: truncateOutput(output, 2000),
		})
	}

	return errors
}

// formatErrors превращает []PlaywrightError в читаемый текст для LLM.
func formatErrors(errors []PlaywrightError) string {
	if len(errors) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, e := range errors {
		if e.TestName != "" {
			sb.WriteString(fmt.Sprintf("Тест %d: %s\n", i+1, e.TestName))
		}
		if e.Message != "" {
			sb.WriteString(fmt.Sprintf("Ошибка: %s\n", e.Message))
		}
		if e.Location != "" {
			sb.WriteString(fmt.Sprintf("Место: %s\n", e.Location))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// truncateOutput обрезает длинный вывод чтобы не перегружать контекст LLM.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Берём конец — там обычно самое важное (итоговые ошибки)
	return "...(truncated)...\n" + s[len(s)-maxLen:]
}
