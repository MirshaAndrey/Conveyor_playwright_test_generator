package main

import (
	"fmt"
	"time"
)

// runBatch выполняет пакетную генерацию последовательно с трекером прогресса.
func runBatch(baseURL, model string, tasks []string, noSave bool) {
	todo := NewTodoList(tasks)

	fmt.Printf("\nПакетная генерация: %d тест-кейсов\n", len(tasks))
	fmt.Printf("Модель: %s\n\n", model)

	todo.Render()

	for i := range todo.Items {
		todo.SetStatus(i, StatusRunning)

		start := time.Now()
		content, err := generate(baseURL, model, todo.Items[i].Task)
		elapsed := time.Since(start)
		todo.SetElapsed(i, elapsed)

		if err != nil {
			todo.SetStatus(i, StatusFailed, err.Error())
			continue
		}

		savedPath := ""
		if !noSave {
			savedPath = autoSavePath(todo.Items[i].Task)
			if saveErr := saveResult(savedPath, content); saveErr != nil {
				todo.SetStatus(i, StatusFailed, saveErr.Error())
				continue
			}
		}

		todo.SetStatus(i, StatusDone, savedPath)
	}

	todo.Summary()
}
