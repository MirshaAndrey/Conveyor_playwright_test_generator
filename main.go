package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	taskFlag := flag.String("task", "", "Тест-кейс (если не указано — интерактивный режим)")
	tasksFileFlag := flag.String("tasks-file", "", "Файл со списком тест-кейсов (по одному на строку)")
	modelFlag := flag.String("model", "mistral-small3.2:latest", "Имя модели Ollama")
	outFlag := flag.String("out", "", "Файл для сохранения результата")
	baseURL := flag.String("url", "http://localhost:11434", "Адрес Ollama")
	noSave := flag.Bool("no-save", false, "Не сохранять в ./TestCasesTS/ автоматически")
	flag.Parse()

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\033[36m") // cyan
	fmt.Println("   ██████╗ ██████╗ ███╗   ██╗██╗   ██╗███████╗██╗   ██╗ ██████╗ ██████╗ ")
	fmt.Println("  ██╔════╝██╔═══██╗████╗  ██║██║   ██║██╔════╝╚██╗ ██╔╝██╔═══██╗██╔══██╗")
	fmt.Println("  ██║     ██║   ██║██╔██╗ ██║██║   ██║█████╗   ╚████╔╝ ██║   ██║██████╔╝")
	fmt.Println("  ██║     ██║   ██║██║╚██╗██║╚██╗ ██╔╝██╔══╝    ╚██╔╝  ██║   ██║██╔══██╗")
	fmt.Println("  ╚██████╗╚██████╔╝██║ ╚████║ ╚████╔╝ ███████╗   ██║   ╚██████╔╝██║  ██║")
	fmt.Println("   ╚═════╝ ╚═════╝ ╚═╝  ╚═══╝  ╚═══╝  ╚══════╝   ╚═╝    ╚═════╝ ╚═╝  ╚═╝")
	fmt.Println("\033[33m        Playwright Test Generator\033[90m  v1.0.0\033[0m")
	fmt.Println()

	// Проверка Ollama
	fmt.Printf("[*] Проверяю подключение к Ollama (%s)...\n", *baseURL)
	if err := checkOllama(*baseURL); err != nil {
		fmt.Printf("\033[31m[ERR]\033[0m Ollama недоступна: %v\n", err)
		fmt.Println("[TIP] Запусти сервер командой: ollama serve")
		os.Exit(1)
	}
	fmt.Println("\033[32m[OK]\033[0m Ollama доступна.")

	// Выбор модели
	chosenModel := *modelFlag
	modelExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "model" {
			modelExplicit = true
		}
	})
	if !modelExplicit {
		models, err := fetchModels(*baseURL)
		if err != nil {
			fmt.Printf("\033[33m[WARN]\033[0m Не удалось получить список моделей: %v\n", err)
		} else if len(models) == 0 {
			fmt.Println("\033[33m[WARN]\033[0m Нет установленных моделей. Используется значение по умолчанию.")
		} else {
			chosenModel = pickModel(reader, models, chosenModel)
		}
	}

	// ── Пакетный режим ───────────────────────────────────────────────────────
	if *tasksFileFlag != "" {
		tasks, err := loadTasksFile(*tasksFileFlag)
		if err != nil {
			fmt.Printf("\033[31m[ERR]\033[0m Не удалось загрузить файл задач: %v\n", err)
			os.Exit(1)
		}
		runBatch(*baseURL, chosenModel, tasks, *noSave)
		return
	}

	// ── Одиночный / интерактивный режим ──────────────────────────────────────
	fmt.Printf("\n[MODEL] %s\n", chosenModel)

	singleRun := *taskFlag != ""

	for {
		var taskText string

		if singleRun {
			taskText = *taskFlag
		} else {
			fmt.Print("\n> Тест-кейс (или 'q' для выхода): ")
			input, _ := reader.ReadString('\n')
			taskText = strings.TrimSpace(input)

			switch taskText {
			case "":
				fmt.Println("\033[33m[WARN]\033[0m Пустой ввод, попробуй снова.")
				continue
			case "q", "quit", "exit":
				fmt.Println("\nВыход. Пока!")
				return
			}
		}

		fmt.Printf("\n[...] Генерирую тест...\n\n")
		start := time.Now()

		content, err := generateWithStream(*baseURL, chosenModel, taskText)
		if err != nil {
			fmt.Printf("\033[31m[ERR]\033[0m %v\n", err)
			if singleRun {
				os.Exit(1)
			}
			continue
		}

		fmt.Printf("\n\n\033[32m[OK]\033[0m Готово за %.1f сек.\n", time.Since(start).Seconds())

		switch {
		case *outFlag != "":
			if err := saveResult(*outFlag, content); err != nil {
				fmt.Printf("\033[31m[ERR]\033[0m Не удалось сохранить: %v\n", err)
			} else {
				fmt.Printf("\033[32m[SAVE]\033[0m Тест сохранён: %s\n", *outFlag)
			}
		case !*noSave:
			path := autoSavePath(taskText)
			if err := saveResult(path, content); err != nil {
				fmt.Printf("\033[33m[WARN]\033[0m Автосохранение не удалось: %v\n", err)
			} else {
				fmt.Printf("\033[32m[SAVE]\033[0m %s\n", path)
			}
		}

		if singleRun {
			break
		}

		fmt.Println("\n────────────────────────────")
	}
}
