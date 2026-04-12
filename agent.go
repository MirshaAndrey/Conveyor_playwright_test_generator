package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ─── Конфигурация агента ──────────────────────────────────────────────────────

// AgentConfig — параметры агентного режима.
type AgentConfig struct {
	BatchConfig
	MaxFixAttempts int  // сколько раз пытаться починить упавший тест (дефолт: 3)
	NoRun          bool // только генерация без запуска теста
	POMRefactor    bool // post-batch POM рефакторинг после всех кейсов
}

// ─── Агентный loop ────────────────────────────────────────────────────────────

// runAgent — главная функция агентного режима.
// Для каждого кейса: scan → generate → review → run → fix loop.
// После всех кейсов опционально: POM рефакторинг.
func runAgent(cfg AgentConfig, cases []TestCase) {
	todo := NewTodoListFromCases(cases)

	// Шапка
	fmt.Printf("\nАгентный режим: %d тест-кейсов\n", len(cases))
	fmt.Printf("Провайдер: %s | Модель: %s", cfg.Provider, cfg.Model)
	if cfg.ReviewMode {
		fmt.Print(" | Reviewer: on")
	}
	if !cfg.NoRun {
		fmt.Printf(" | Fix attempts: %d", cfg.MaxFixAttempts)
	}
	fmt.Println()

	// Подсчёт разделов
	sections := make(map[string]int)
	var sectionOrder []string
	for _, tc := range cases {
		if sections[tc.Section] == 0 {
			sectionOrder = append(sectionOrder, tc.Section)
		}
		sections[tc.Section]++
	}
	var sectionParts []string
	for _, s := range sectionOrder {
		sectionParts = append(sectionParts, fmt.Sprintf("%s(%d)", s, sections[s]))
	}
	fmt.Printf("Разделы: %s\n\n", strings.Join(sectionParts, " "))

	todo.Render()

	// Сохраняем пути всех успешных файлов для post-batch POM
	successFiles := make(map[string]string) // path → content

	for i, tc := range cases {
		todo.SetStatus(i, StatusRunning)
		start := time.Now()

		savedPath, ok := processOneCase(cfg, todo, i, tc)

		todo.SetElapsed(i, time.Since(start))

		if !ok {
			continue
		}

		// Читаем содержимое для POM рефакторинга
		if cfg.POMRefactor && savedPath != "" {
			if data, err := os.ReadFile(savedPath); err == nil {
				successFiles[savedPath] = string(data)
			}
		}

		rateLimitIfNeeded(cfg.BatchConfig, i < len(cases)-1)
	}

	todo.Summary()

	// Post-batch POM рефакторинг
	if cfg.POMRefactor && len(successFiles) > 1 {
		runPOMRefactor(cfg, successFiles)
	}
}

// processOneCase выполняет полный цикл для одного кейса.
// Возвращает путь к сохранённому файлу и флаг успеха.
func processOneCase(cfg AgentConfig, todo *TodoList, idx int, tc TestCase) (string, bool) {
	// ── 1. Сканирование ──────────────────────────────────────────────────────
	scanHint := tc.PromptHint
	if tc.URL != "" {
		todo.SetStatus(idx, StatusScanning)
		scanResult, err := runScanner(tc.URL)
		if err != nil {
			fmt.Printf("\033[33m[WARN]\033[0m scan %s: %v\n", tc.Name, err)
		} else {
			scanned := formatScanContext(scanResult)
			if scanHint != "" {
				scanHint = scanned + "\n\n" + scanHint
			} else {
				scanHint = scanned
			}
		}
		todo.SetStatus(idx, StatusRunning)
	}

	// ── 2. Генерация + валидация + ревью ─────────────────────────────────────
	tcWithHint := tc
	tcWithHint.PromptHint = scanHint

	content, err := generateOne(cfg.BatchConfig, todo, idx, buildCasePrompt(tcWithHint))
	if err != nil {
		todo.SetStatus(idx, StatusFailed, err.Error())
		return "", false
	}

	// ── 3. Сохранение ────────────────────────────────────────────────────────
	if cfg.NoSave {
		todo.SetStatus(idx, StatusDone, "")
		return "", true
	}

	savedPath := autoSavePathForCase(tc)
	if err := saveResult(savedPath, content); err != nil {
		todo.SetStatus(idx, StatusFailed, err.Error())
		return "", false
	}

	// ── 4. Запуск теста ──────────────────────────────────────────────────────
	if cfg.NoRun {
		todo.SetStatus(idx, StatusDone, savedPath)
		return savedPath, true
	}

	todo.SetStatus(idx, StatusTesting)
	result, err := runPlaywrightTest(savedPath)
	if err != nil {
		todo.SetStatus(idx, StatusFailed, fmt.Sprintf("runner error: %v", err))
		return "", false
	}

	if result.Passed {
		todo.SetStatus(idx, StatusDone, savedPath)
		return savedPath, true
	}

	// ── 5. Fix loop ──────────────────────────────────────────────────────────
	// scanHint передаётся в fixLoop — LLM знает реальные селекторы страницы
	// и не угадывает их вслепую при каждой попытке исправления.
	finalPath, fixed := fixLoop(cfg, todo, idx, savedPath, content, scanHint, result)
	if !fixed {
		todo.SetStatus(idx, StatusFailed, "тест не прошёл после всех попыток исправления")
		return "", false
	}

	return finalPath, true
}

// fixLoop пытается исправить упавший тест до MaxFixAttempts раз.
// scanHint — контекст страницы (селекторы + API) из фазы сканирования;
// передаётся в промпт чтобы LLM не угадывал селекторы вслепую.
func fixLoop(cfg AgentConfig, todo *TodoList, idx int, filePath, code, scanHint string, lastResult RunResult) (string, bool) {
	currentCode := code
	currentResult := lastResult

	for attempt := 1; attempt <= cfg.MaxFixAttempts; attempt++ {
		todo.SetFixStatus(idx, attempt, cfg.MaxFixAttempts)
		rateLimitIfNeeded(cfg.BatchConfig, true)

		errorText := formatErrors(currentResult.Errors)
		if errorText == "" {
			errorText = truncateOutput(currentResult.Output, 1500)
		}

		fixed, err := doFix(cfg.BatchConfig, buildFixPrompt(currentCode, errorText, scanHint))
		if err != nil {
			continue
		}

		fixed = cleanCodeBlock(fixed)
		if validateContent(fixed) != nil {
			continue
		}

		// Сохраняем исправленную версию
		if err := saveResult(filePath, fixed); err != nil {
			continue
		}
		currentCode = fixed

		// Обновляем FixCount в трекере
		todo.mu.Lock()
		todo.Items[idx].FixCount = attempt
		todo.mu.Unlock()

		// Запускаем снова
		todo.SetStatus(idx, StatusTesting)
		result, err := runPlaywrightTest(filePath)
		if err != nil {
			continue
		}

		if result.Passed {
			todo.SetStatus(idx, StatusDone, filePath)
			return filePath, true
		}

		currentResult = result
	}

	return "", false
}

// doFix вызывает LLM для исправления упавшего теста.
func doFix(cfg BatchConfig, fixPrompt string) (string, error) {
	switch {
	case cfg.IsGemini():
		return sendGeminiRequest(cfg.APIKey, cfg.Model, fixPrompt, fixSystemPrompt, false, nil)
	case cfg.IsClaude():
		return sendClaudeRequest(cfg.APIKey, cfg.Model, fixPrompt, fixSystemPrompt, false, nil)
	default:
		return sendRequest(cfg.BaseURL, cfg.Model, fixPrompt, fixSystemPrompt, nil)
	}
}

// ─── Post-batch POM рефакторинг ───────────────────────────────────────────────

// runPOMRefactor отправляет все сгенерированные тесты на POM рефакторинг.
func runPOMRefactor(cfg AgentConfig, files map[string]string) {
	fmt.Println("\n\033[36m[POM]\033[0m Запускаю post-batch рефакторинг...")

	prompt := buildPOMBatchPrompt(files)
	rateLimitIfNeeded(cfg.BatchConfig, true)

	var result string
	var err error
	switch {
	case cfg.IsGemini():
		result, err = sendGeminiRequest(cfg.APIKey, cfg.Model, prompt, pomBatchSystemPrompt, false, nil)
	case cfg.IsClaude():
		result, err = sendClaudeRequest(cfg.APIKey, cfg.Model, prompt, pomBatchSystemPrompt, false, nil)
	default:
		result, err = sendRequest(cfg.BaseURL, cfg.Model, prompt, pomBatchSystemPrompt, nil)
	}

	if err != nil {
		fmt.Printf("\033[31m[POM ERR]\033[0m %v\n", err)
		return
	}

	saved, errs := savePOMFiles(result)
	for _, p := range saved {
		fmt.Printf("\033[32m[POM SAVE]\033[0m %s\n", p)
	}
	for _, e := range errs {
		fmt.Printf("\033[33m[POM WARN]\033[0m %s\n", e)
	}

	if len(saved) > 0 {
		fmt.Printf("\033[32m[POM OK]\033[0m Рефакторинг завершён: %d файлов\n", len(saved))
	}
}

// ─── Парсер POM ответа ────────────────────────────────────────────────────────

// fileSectionRe ищет маркеры --- FILE: path.ts --- в ответе модели.
var fileSectionRe = regexp.MustCompile(`(?m)^---\s*FILE:\s*(.+?)\s*---\s*\n`)

// savePOMFiles разбирает ответ модели с несколькими файлами и сохраняет каждый.
// Ожидаемый формат: --- FILE: pages/LoginPage.ts --- \n<код>
func savePOMFiles(response string) (saved []string, errs []string) {
	matches := fileSectionRe.FindAllStringIndex(response, -1)
	if len(matches) == 0 {
		errs = append(errs, "маркеры --- FILE: --- не найдены в ответе модели")
		return
	}

	for i, match := range matches {
		// Имя файла из маркера
		headerMatch := fileSectionRe.FindStringSubmatch(response[match[0]:match[1]])
		if len(headerMatch) < 2 {
			continue
		}
		name := strings.TrimSpace(headerMatch[1])

		// Содержимое — от конца этого маркера до начала следующего (или конца строки)
		contentStart := match[1]
		var contentEnd int
		if i+1 < len(matches) {
			contentEnd = matches[i+1][0]
		} else {
			contentEnd = len(response)
		}

		code := cleanCodeBlock(strings.TrimSpace(response[contentStart:contentEnd]))
		if code == "" {
			errs = append(errs, fmt.Sprintf("пустое содержимое для %s", name))
			continue
		}

		// Сохраняем в TestCasesTS/pom/<имя>
		outPath := filepath.Join("TestCasesTS", "pom", name)
		if err := saveResult(outPath, code); err != nil {
			errs = append(errs, fmt.Sprintf("ошибка сохранения %s: %v", name, err))
			continue
		}
		saved = append(saved, outPath)
	}
	return
}
