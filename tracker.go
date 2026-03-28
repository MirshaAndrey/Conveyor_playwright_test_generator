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
	StatusPending TodoStatus = iota
	StatusRunning
	StatusDone
	StatusFailed
	StatusSkipped
)

func (s TodoStatus) icon() string {
	switch s {
	case StatusPending:
		return "\033[90m[ ]\033[0m"
	case StatusRunning:
		return "\033[33m[~]\033[0m"
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
	case StatusRunning:
		return "генерация..."
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
	Task      string
	Status    TodoStatus
	Elapsed   time.Duration
	SavedPath string
	Error     string
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
		items[i] = &TodoItem{Task: t, Status: StatusPending}
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

// renderUnsafe выполняет отрисовку (вызывать под мьютексом)
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
		taskLabel := truncate(item.Task, 55)

		switch item.Status {
		case StatusPending:
			fmt.Printf("  %s  %d. %s\n", item.Status.icon(), i+1, taskLabel)
		case StatusRunning:
			fmt.Printf("  \033[33m%s\033[0m  %d. \033[33m%s\033[0m  [%s]\n",
				item.Status.icon(), i+1, taskLabel, item.Status.label())
		case StatusDone:
			savedInfo := ""
			if item.SavedPath != "" {
				savedInfo = "  → " + filepath.Base(item.SavedPath)
			}
			fmt.Printf("  %s  %d. %s  \033[90m%.1fs%s\033[0m\n",
				item.Status.icon(), i+1, taskLabel, item.Elapsed.Seconds(), savedInfo)
		case StatusFailed:
			errInfo := ""
			if item.Error != "" {
				errInfo = "  (" + truncate(item.Error, 30) + ")"
			}
			fmt.Printf("  %s  %d. \033[31m%s\033[0m\033[90m%s\033[0m\n",
				item.Status.icon(), i+1, taskLabel, errInfo)
		case StatusSkipped:
			fmt.Printf("  %s  %d. \033[90m%s\033[0m\n",
				item.Status.icon(), i+1, taskLabel)
		}
		lines++
	}

	fmt.Println()
	lines++

	tl.rendered = lines
}

// Render — потокобезопасная отрисовка
func (tl *TodoList) Render() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.renderUnsafe()
}

// SetStatus обновляет статус и перерисовывает список
func (tl *TodoList) SetStatus(idx int, status TodoStatus, opts ...string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	item := tl.Items[idx]
	item.Status = status
	if status == StatusDone || status == StatusFailed {
		if len(opts) > 0 {
			if status == StatusDone {
				item.SavedPath = opts[0]
			} else {
				item.Error = opts[0]
			}
		}
	}
	tl.renderUnsafe()
}

// SetElapsed фиксирует время выполнения задачи
func (tl *TodoList) SetElapsed(idx int, d time.Duration) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.Items[idx].Elapsed = d
}

// Summary печатает итоговую сводку
func (tl *TodoList) Summary() {
	done, failed, skipped := 0, 0, 0
	var totalElapsed time.Duration
	for _, item := range tl.Items {
		switch item.Status {
		case StatusDone:
			done++
			totalElapsed += item.Elapsed
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
	fmt.Printf("  Время генерации:  %s\n", totalElapsed.Round(time.Second))
	fmt.Printf("  Время сессии:     %s\n", sessionElapsed)
	fmt.Println("══════════════════════════════════════")
}
