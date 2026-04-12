package main

import (
	"fmt"
	"strings"
	"time"
)

// ─── Конфигурация запуска ─────────────────────────────────────────────────────

// BatchConfig — всё что нужно знать одному запуску генерации.
type BatchConfig struct {
	Provider     string
	APIKey       string
	BaseURL      string
	Model        string
	SystemPrompt string // итоговый промпт после подстановки {{context}}
	NoSave       bool
	ReviewMode   bool
	MaxRetries   int
}

// IsGemini возвращает true если активный провайдер — Google Gemini.
func (c BatchConfig) IsGemini() bool {
	return c.Provider == "gemini"
}

// IsClaude возвращает true если активный провайдер — Anthropic Claude.
func (c BatchConfig) IsClaude() bool {
	return c.Provider == "claude"
}

// ─── Вспомогательные функции генерации ───────────────────────────────────────

// doGenerate вызывает нужный провайдер тихо (без стриминга).
func doGenerate(cfg BatchConfig, prompt string) (string, error) {
	switch {
	case cfg.IsGemini():
		return sendGeminiRequest(cfg.APIKey, cfg.Model, prompt, cfg.SystemPrompt, false, nil)
	case cfg.IsClaude():
		return sendClaudeRequest(cfg.APIKey, cfg.Model, prompt, cfg.SystemPrompt, false, nil)
	default:
		return sendRequest(cfg.BaseURL, cfg.Model, prompt, cfg.SystemPrompt, nil)
	}
}

// doReview вызывает провайдер для второго прохода ревью.
func doReview(cfg BatchConfig, reviewPrompt string) (string, error) {
	switch {
	case cfg.IsGemini():
		return sendGeminiRequest(cfg.APIKey, cfg.Model, reviewPrompt, reviewSystemPrompt, false, nil)
	case cfg.IsClaude():
		return sendClaudeRequest(cfg.APIKey, cfg.Model, reviewPrompt, reviewSystemPrompt, false, nil)
	default:
		return sendRequest(cfg.BaseURL, cfg.Model, reviewPrompt, reviewSystemPrompt, nil)
	}
}

// rateLimitIfNeeded делает паузу между запросами Gemini (бесплатный тариф).
// Вызывать только после успешного запроса и только если есть следующий.
func rateLimitIfNeeded(cfg BatchConfig, hasNext bool) {
	if cfg.IsGemini() && hasNext {
		time.Sleep(3 * time.Second)
	}
}

// ─── Ядро генерации одного кейса ─────────────────────────────────────────────

// generateOne выполняет полный цикл для одного тест-кейса:
// retry при ошибке валидации → очистка markdown → опциональный review-проход.
func generateOne(cfg BatchConfig, todo *TodoList, idx int, userPrompt string) (string, error) {
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	var content string
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			todo.SetRetry(idx, attempt)
			todo.SetStatus(idx, StatusRunning)
			rateLimitIfNeeded(cfg, true)
		}

		var err error
		content, err = doGenerate(cfg, userPrompt)
		if err != nil {
			lastErr = err
			continue
		}

		content = cleanCodeBlock(content)

		if valErr := validateContent(content); valErr != nil {
			lastErr = valErr
			continue
		}

		lastErr = nil
		break
	}

	if lastErr != nil {
		return "", lastErr
	}

	if cfg.ReviewMode {
		todo.SetStatus(idx, StatusReviewing)
		rateLimitIfNeeded(cfg, true)

		reviewed, err := doReview(cfg, buildReviewPrompt(userPrompt, content))
		if err == nil {
			reviewed = cleanCodeBlock(reviewed)
			if validateContent(reviewed) == nil {
				content = reviewed
			}
			// Если ревью вернуло невалидный результат — оставляем оригинал
		}
	}

	return content, nil
}

// ─── Шапка пакетного режима ───────────────────────────────────────────────────

// printBatchHeader выводит информацию о запуске. sections — опциональная строка разделов.
func printBatchHeader(cfg BatchConfig, total int, sections string) {
	fmt.Printf("\nПакетная генерация: %d тест-кейсов\n", total)

	var meta []string
	meta = append(meta, "Провайдер: "+cfg.Provider)
	meta = append(meta, "Модель: "+cfg.Model)
	if cfg.ReviewMode {
		meta = append(meta, "Режим: Generator→Reviewer")
	}
	fmt.Println(strings.Join(meta, " | "))

	if sections != "" {
		fmt.Println("Разделы:  ", sections)
	}
	fmt.Println()
}

// ─── Пакетный режим: txt ──────────────────────────────────────────────────────

func runBatch(cfg BatchConfig, tasks []string) {
	todo := NewTodoList(tasks)
	printBatchHeader(cfg, len(tasks), "")
	todo.Render()

	for i := range todo.Items {
		todo.SetStatus(i, StatusRunning)
		start := time.Now()

		content, err := generateOne(cfg, todo, i, todo.Items[i].Task)
		todo.SetElapsed(i, time.Since(start))

		if err != nil {
			todo.SetStatus(i, StatusFailed, err.Error())
			continue
		}

		savedPath := ""
		if !cfg.NoSave {
			savedPath = autoSavePath(todo.Items[i].Task)
			if saveErr := saveResult(savedPath, content); saveErr != nil {
				todo.SetStatus(i, StatusFailed, saveErr.Error())
				continue
			}
		}

		rateLimitIfNeeded(cfg, i < len(todo.Items)-1)
		todo.SetStatus(i, StatusDone, savedPath)
	}

	todo.Summary()
}

// ─── Пакетный режим: cases.json ───────────────────────────────────────────────

func runBatchCases(cfg BatchConfig, cases []TestCase) {
	todo := NewTodoListFromCases(cases)

	// Подсчёт кейсов по разделам (порядок первого вхождения сохраняется)
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

	printBatchHeader(cfg, len(cases), strings.Join(sectionParts, " "))
	todo.Render()

	for i, tc := range cases {
		todo.SetStatus(i, StatusRunning)
		start := time.Now()

		content, err := generateOne(cfg, todo, i, buildCasePrompt(tc))
		todo.SetElapsed(i, time.Since(start))

		if err != nil {
			todo.SetStatus(i, StatusFailed, err.Error())
			continue
		}

		savedPath := ""
		if !cfg.NoSave {
			savedPath = autoSavePathForCase(tc)
			if saveErr := saveResult(savedPath, content); saveErr != nil {
				todo.SetStatus(i, StatusFailed, saveErr.Error())
				continue
			}
		}

		rateLimitIfNeeded(cfg, i < len(cases)-1)
		todo.SetStatus(i, StatusDone, savedPath)
	}

	todo.Summary()
}
