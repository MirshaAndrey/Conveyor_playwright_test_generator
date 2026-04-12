package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ─── Статусы задач ────────────────────────────────────────────────────────────

type TodoStatus int

const (
	StatusPending   TodoStatus = iota
	StatusScanning             // сканирование страницы
	StatusRunning              // генерация теста
	StatusReviewing            // второй проход ревью
	StatusTesting              // запуск теста через Playwright
	StatusFixing               // LLM исправляет ошибку теста
	StatusDone
	StatusFailed
	StatusSkipped // зарезервировано
)

func (s TodoStatus) icon() string {
	switch s {
	case StatusPending:
		return "\033[90m[ ]\033[0m"
	case StatusScanning:
		return "\033[35m[⊕]\033[0m"
	case StatusRunning:
		return "\033[33m[~]\033[0m"
	case StatusReviewing:
		return "\033[36m[»]\033[0m"
	case StatusTesting:
		return "\033[34m[►]\033[0m"
	case StatusFixing:
		return "\033[33m[↻]\033[0m"
	case StatusDone:
		return "\033[32m[+]\033[0m"
	case StatusFailed:
		return "\033[31m[x]\033[0m"
	case StatusSkipped:
		return "\033[90m[-]\033[0m"
	}
	return "[?]"
}

func (s TodoStatus) label() string {
	switch s {
	case StatusPending:
		return "ожидает"
	case StatusScanning:
		return "сканирование..."
	case StatusRunning:
		return "генерация..."
	case StatusReviewing:
		return "ревью..."
	case StatusTesting:
		return "запуск теста..."
	case StatusFixing:
		return "исправление..."
	case StatusDone:
		return "готово"
	case StatusFailed:
		return "ошибка"
	case StatusSkipped:
		return "пропущено"
	}
	return ""
}

// ─── Элемент и список ─────────────────────────────────────────────────────────

type TodoItem struct {
	Name      string     // короткое имя для отображения в трекере
	Task      string     // полный текст задачи для промпта
	Status    TodoStatus
	Elapsed   time.Duration
	SavedPath string
	Error     string
	Retries   int    // количество retry при валидации
	FixHint   string // суффикс для StatusFixing: "2/3"
	FixCount  int    // сколько fix-итераций потребовалось
}

type TodoList struct {
	Items     []*TodoItem
	startTime time.Time
	rendered  int
	mu        sync.Mutex
}

func NewTodoList(tasks []string) *TodoList {
	items := make([]*TodoItem, len(tasks))
	for i, t := range tasks {
		items[i] = &TodoItem{Name: t, Task: t, Status: StatusPending}
	}
	return &TodoList{Items: items, startTime: time.Now()}
}

func NewTodoListFromCases(cases []TestCase) *TodoList {
	items := make([]*TodoItem, len(cases))
	for i, tc := range cases {
		items[i] = &TodoItem{
			Name:   fmt.Sprintf("[%s] %s", tc.Section, tc.Name),
			Task:   tc.Task,
			Status: StatusPending,
		}
	}
	return &TodoList{Items: items, startTime: time.Now()}
}

// ─── Отрисовка ────────────────────────────────────────────────────────────────

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func progressBar(done, total, width int) string {
	if total == 0 {
		return ""
	}
	filled := done * width / total
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	pct := done * 100 / total
	return fmt.Sprintf("[%s] %d/%d (%d%%)", bar, done, total, pct)
}

func (tl *TodoList) renderUnsafe() {
	if tl.rendered > 0 {
		fmt.Printf("\033[%dA", tl.rendered)
		for i := 0; i < tl.rendered; i++ {
			fmt.Print("\033[2K\n")
		}
		fmt.Printf("\033[%dA", tl.rendered)
	}

	lines := 0

	done, failed, skipped := 0, 0, 0
	for _, item := range tl.Items {
		switch item.Status {
		case StatusDone:
			done++
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}
	completed := done + failed + skipped
	total := len(tl.Items)

	bar := progressBar(completed, total, 20)
	elapsed := time.Since(tl.startTime).Round(time.Second)
	fmt.Printf("  %s  %s\n", bar, elapsed)
	lines++

	fmt.Println()
	lines++

	for i, item := range tl.Items {
		label := truncate(item.Name, 52)

		switch item.Status {
		case StatusPending:
			fmt.Printf("  %s  %d. %s\n", item.Status.icon(), i+1, label)

		case StatusScanning:
			fmt.Printf("  %s  %d. \033[35m%s\033[0m  \033[90m[%s]\033[0m\n",
				item.Status.icon(), i+1, label, item.Status.label())

		case StatusRunning:
			retryInfo := ""
			if item.Retries > 0 {
				retryInfo = fmt.Sprintf(" retry %d", item.Retries)
			}
			fmt.Printf("  %s  %d. \033[33m%s\033[0m  \033[90m[%s%s]\033[0m\n",
				item.Status.icon(), i+1, label, item.Status.label(), retryInfo)

		case StatusReviewing:
			fmt.Printf("  %s  %d. \033[36m%s\033[0m  \033[90m[%s]\033[0m\n",
				item.Status.icon(), i+1, label, item.Status.label())

		case StatusTesting:
			fmt.Printf("  %s  %d. \033[34m%s\033[0m  \033[90m[%s]\033[0m\n",
				item.Status.icon(), i+1, label, item.Status.label())

		case StatusFixing:
			fmt.Printf("  %s  %d. \033[33m%s\033[0m  \033[90m[%s %s]\033[0m\n",
				item.Status.icon(), i+1, label, item.Status.label(), item.FixHint)

		case StatusDone:
			savedInfo := ""
			if item.SavedPath != "" {
				savedInfo = "  → " + filepath.Base(item.SavedPath)
			}
			retryInfo := ""
			if item.Retries > 0 {
				retryInfo = fmt.Sprintf(" (%d retry)", item.Retries)
			}
			fmt.Printf("  %s  %d. %s  \033[90m%.1fs%s%s\033[0m\n",
				item.Status.icon(), i+1, label, item.Elapsed.Seconds(), savedInfo, retryInfo)

		case StatusFailed:
			errInfo := ""
			if item.Error != "" {
				errInfo = "  (" + truncate(item.Error, 35) + ")"
			}
			fmt.Printf("  %s  %d. \033[31m%s\033[0m\033[90m%s\033[0m\n",
				item.Status.icon(), i+1, label, errInfo)

		case StatusSkipped:
			fmt.Printf("  %s  %d. \033[90m%s\033[0m\n",
				item.Status.icon(), i+1, label)
		}
		lines++
	}

	fmt.Println()
	lines++

	tl.rendered = lines
}

func (tl *TodoList) Render() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.renderUnsafe()
}

func (tl *TodoList) SetStatus(idx int, status TodoStatus, opts ...string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	item := tl.Items[idx]
	item.Status = status
	if status == StatusDone && len(opts) > 0 {
		item.SavedPath = opts[0]
	} else if status == StatusFailed && len(opts) > 0 {
		item.Error = opts[0]
	}
	tl.renderUnsafe()
}

func (tl *TodoList) SetRetry(idx, count int) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.Items[idx].Retries = count
	tl.renderUnsafe()
}

func (tl *TodoList) SetElapsed(idx int, d time.Duration) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.Items[idx].Elapsed = d
}

func (tl *TodoList) Summary() {
	done, failed, skipped, totalRetries, totalFixes := 0, 0, 0, 0, 0
	var totalElapsed time.Duration
	for _, item := range tl.Items {
		switch item.Status {
		case StatusDone:
			done++
			totalElapsed += item.Elapsed
			totalRetries += item.Retries
			totalFixes += item.FixCount
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}

	sessionElapsed := time.Since(tl.startTime).Round(time.Second)

	fmt.Println("\n══════════════════════════════════════")
	fmt.Println(" Итог сессии")
	fmt.Println("══════════════════════════════════════")
	fmt.Printf("  \033[32mУспешно:\033[0m    %d\n", done)
	if failed > 0 {
		fmt.Printf("  \033[31mОшибки:\033[0m     %d\n", failed)
	}
	if skipped > 0 {
		fmt.Printf("  \033[90mПропущено:\033[0m  %d\n", skipped)
	}
	if totalRetries > 0 {
		fmt.Printf("  Retry (валид.):  %d\n", totalRetries)
	}
	if totalFixes > 0 {
		fmt.Printf("  Fix (playwright): %d\n", totalFixes)
	}
	fmt.Printf("  Время генерации:  %s\n", totalElapsed.Round(time.Second))
	fmt.Printf("  Время сессии:     %s\n", sessionElapsed)
	fmt.Println("══════════════════════════════════════")
}

// SetSubStatus обновляет статус без полного ре-рендера метки — для fix-loop.
// hint отображается как суффикс: "[↻] исправление... 2/3"
func (tl *TodoList) SetFixStatus(idx, attempt, maxAttempts int) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	item := tl.Items[idx]
	item.Status = StatusFixing
	item.FixHint = fmt.Sprintf("%d/%d", attempt, maxAttempts)
	tl.renderUnsafe()
}
