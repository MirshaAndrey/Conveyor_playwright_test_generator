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
	configFlag := flag.String("config", "config.json", "Путь к файлу конфигурации")
	taskFlag := flag.String("task", "", "Тест-кейс (если не указано — интерактивный режим)")
	tasksFileFlag := flag.String("tasks-file", "", "Файл со списком тест-кейсов (.txt)")
	casesFileFlag := flag.String("cases-file", "", "Файл тест-кейсов в JSON формате (.json)")
	modelFlag := flag.String("model", "", "Имя модели (зависит от провайдера)")
	providerFlag := flag.String("provider", "", "Провайдер LLM: ollama или gemini")
	keyFlag := flag.String("key", "", "API ключ Gemini (или переменная GEMINI_API_KEY)")
	outFlag := flag.String("out", "", "Файл для сохранения результата (одиночный режим)")
	baseURL := flag.String("url", "http://localhost:11434", "Адрес Ollama")
	noSave := flag.Bool("no-save", false, "Не сохранять файлы автоматически")
	reviewFlag := flag.Bool("review", false, "Включить второй проход ревью (Generator→Reviewer)")
	scanURL := flag.String("scan-url", "", "URL для сканирования data-testid/aria-label через Playwright (headless)")
	agentFlag := flag.Bool("agent", false, "Агентный режим: scan→generate→review→run→fix")
	maxFixFlag := flag.Int("max-fix", 3, "Агентный режим: максимум попыток исправления упавшего теста")
	noRunFlag := flag.Bool("no-run", false, "Агентный режим: не запускать тесты (только генерация)")
	pomFlag := flag.Bool("pom", false, "Агентный режим: post-batch POM рефакторинг после всех кейсов")
	flag.Parse()

	// ── Загрузка конфигурации ────────────────────────────────────────────────
	var config Config
	if cfg, err := loadConfig(*configFlag); err == nil {
		config = *cfg
	} else if !os.IsNotExist(err) {
		fmt.Printf("\033[33m[WARN]\033[0m Ошибка чтения %s: %v\n", *configFlag, err)
	} else {
		fmt.Printf("\033[90m[INFO]\033[0m %s не найден, используются дефолтные настройки\033[0m\n", *configFlag)
	}

	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	// Приоритет флагов над конфигом
	if !setFlags["provider"] && config.Provider != "" {
		*providerFlag = config.Provider
	}
	if !setFlags["key"] && config.GeminiAPIKey != "" {
		*keyFlag = config.GeminiAPIKey
	}
	if !setFlags["key"] && config.ClaudeAPIKey != "" {
		*keyFlag = config.ClaudeAPIKey
	}
	if !setFlags["url"] && config.OllamaURL != "" {
		*baseURL = config.OllamaURL
	}

	// ── Сканирование страницы (если указан -scan-url) ────────────────────────
	var scanContext string
	if *scanURL != "" {
		scanResult, err := runScanner(*scanURL)
		if err != nil {
			fmt.Printf("\033[31m[ERR]\033[0m Сканирование не удалось: %v\n", err)
			os.Exit(1)
		}
		scanContext = formatScanContext(scanResult)
	}

	// Итоговый системный промпт: конфиг или дефолт, с подстановкой {{context}}.
	// Контекст приложения применяется ОДИН РАЗ — здесь, через {{context}}.
	// Если есть результат сканирования — он добавляется к ручному контексту.
	rawPrompt := config.SystemPrompt
	if rawPrompt == "" {
		rawPrompt = defaultSystemPrompt
	}
	finalContext := config.Context
	if scanContext != "" {
		if finalContext != "" {
			finalContext += "\n\n"
		}
		finalContext += scanContext
	}
	resolvedSystemPrompt := applySystemPromptTemplate(rawPrompt, finalContext)

	// MaxRetries: из конфига или константа-дефолт.
	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	reviewMode := *reviewFlag || config.ReviewMode

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\033[36m")
	fmt.Println("\033[33m   CONVEYOR Playwright Test Generator\033[90m  v1.1.0\033[0m")
	fmt.Println()

	// ── Выбор провайдера ─────────────────────────────────────────────────────
	chosenProvider := *providerFlag
	if chosenProvider == "" {
		fmt.Println("Доступные провайдеры:")
		fmt.Println("  1. ollama (локально)")
		fmt.Println("  2. gemini (Google API)")
		fmt.Println("  3. claude (Anthropic API)")
		fmt.Print("\nВведи номер провайдера [по умолчанию: ollama]: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		switch {
		case input == "2" || strings.ToLower(input) == "gemini":
			chosenProvider = "gemini"
		case input == "3" || strings.ToLower(input) == "claude":
			chosenProvider = "claude"
		default:
			chosenProvider = "ollama"
		}
	}
	chosenProvider = strings.ToLower(chosenProvider)

	var chosenModel string
	var apiKey string

	switch chosenProvider {
	case "gemini":
		apiKey = *keyFlag
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
		}
		if apiKey == "" {
			fmt.Println("\033[31m[ERR]\033[0m Не задан API ключ (флаг -key или переменная GEMINI_API_KEY)")
			os.Exit(1)
		}
		if setFlags["model"] {
			chosenModel = *modelFlag
		} else if config.GeminiModel != "" {
			chosenModel = config.GeminiModel
		} else {
			chosenModel = "gemini-2.5-flash"
		}
		fmt.Println("\033[32m[OK]\033[0m Gemini API.")

	case "claude":
		apiKey = *keyFlag
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			fmt.Println("\033[31m[ERR]\033[0m Не задан API ключ (флаг -key или переменная ANTHROPIC_API_KEY)")
			os.Exit(1)
		}
		if setFlags["model"] {
			chosenModel = *modelFlag
		} else if config.ClaudeModel != "" {
			chosenModel = config.ClaudeModel
		} else {
			chosenModel = pickModel(reader, claudeModels, claudeDefaultModel)
		}
		fmt.Println("\033[32m[OK]\033[0m Claude API.")

	default: // ollama
		fmt.Printf("[*] Проверяю подключение к Ollama (%s)...\n", *baseURL)
		if err := checkOllama(*baseURL); err != nil {
			fmt.Printf("\033[31m[ERR]\033[0m Ollama недоступна: %v\n", err)
			fmt.Println("[TIP] Запусти сервер: ollama serve")
			os.Exit(1)
		}
		fmt.Println("\033[32m[OK]\033[0m Ollama доступна.")

		chosenModel = "mistral-small3.2:latest"
		if config.OllamaModel != "" {
			chosenModel = config.OllamaModel
		}
		if setFlags["model"] {
			chosenModel = *modelFlag
		} else {
			models, err := fetchModels(*baseURL)
			if err != nil {
				fmt.Printf("\033[33m[WARN]\033[0m Не удалось получить список моделей: %v\n", err)
			} else if len(models) == 0 {
				fmt.Println("\033[33m[WARN]\033[0m Нет установленных моделей.")
			} else {
				chosenModel = pickModel(reader, models, chosenModel)
			}
		}
	}

	batchCfg := BatchConfig{
		Provider:     chosenProvider,
		APIKey:       apiKey,
		BaseURL:      *baseURL,
		Model:        chosenModel,
		SystemPrompt: resolvedSystemPrompt,
		NoSave:       *noSave,
		ReviewMode:   reviewMode,
		MaxRetries:   maxRetries,
	}

	// ── Агентный режим ──────────────────────────────────────────────────────
	if *agentFlag {
		if *casesFileFlag == "" {
			fmt.Println("\033[31m[ERR]\033[0m Агентный режим требует -cases-file")
			os.Exit(1)
		}
		cases, err := loadCasesJSON(*casesFileFlag)
		if err != nil {
			fmt.Printf("\033[31m[ERR]\033[0m %v\n", err)
			os.Exit(1)
		}
		agentCfg := AgentConfig{
			BatchConfig:    batchCfg,
			MaxFixAttempts: *maxFixFlag,
			NoRun:          *noRunFlag,
			POMRefactor:    *pomFlag,
		}
		runAgent(agentCfg, cases)
		return
	}

	// ── JSON cases режим ──────────────────────────────────────────────────────
	if *casesFileFlag != "" {
		cases, err := loadCasesJSON(*casesFileFlag)
		if err != nil {
			fmt.Printf("\033[31m[ERR]\033[0m %v\n", err)
			os.Exit(1)
		}
		runBatchCases(batchCfg, cases)
		return
	}

	// ── Пакетный txt режим ────────────────────────────────────────────────────
	if *tasksFileFlag != "" {
		tasks, err := loadTasksFile(*tasksFileFlag)
		if err != nil {
			fmt.Printf("\033[31m[ERR]\033[0m %v\n", err)
			os.Exit(1)
		}
		runBatch(batchCfg, tasks)
		return
	}

	// ── Одиночный / интерактивный режим ──────────────────────────────────────
	fmt.Printf("\n[MODEL] %s  [PROVIDER] %s", chosenModel, chosenProvider)
	if reviewMode {
		fmt.Print("  [REVIEW] on")
	}
	fmt.Println()

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

		// Контекст уже встроен в системный промпт — передаём только задачу.
		userPrompt := buildSimplePrompt(taskText)

		fmt.Printf("\n[...] Генерирую тест...\n\n")
		start := time.Now()

		var content string
		var err error

		switch chosenProvider {
		case "gemini":
			content, err = geminiGenerateWithStream(apiKey, chosenModel, userPrompt, resolvedSystemPrompt)
		case "claude":
			content, err = claudeGenerateWithStream(apiKey, chosenModel, userPrompt, resolvedSystemPrompt)
		default:
			content, err = ollamaGenerateWithStream(*baseURL, chosenModel, userPrompt, resolvedSystemPrompt)
		}
		if err != nil {
			fmt.Printf("\033[31m[ERR]\033[0m %v\n", err)
			if singleRun {
				os.Exit(1)
			}
			continue
		}

		content = cleanCodeBlock(content)

		if valErr := validateContent(content); valErr != nil {
			fmt.Printf("\033[33m[WARN]\033[0m %v\n", valErr)
		}

		if reviewMode {
			fmt.Printf("\n[»] Запускаю ревью...\n")
			reviewed, revErr := doReview(batchCfg, buildReviewPrompt(userPrompt, content))
			if revErr == nil {
				reviewed = cleanCodeBlock(reviewed)
				if validateContent(reviewed) == nil {
					content = reviewed
					fmt.Printf("\033[36m[»]\033[0m Ревью применено.\n")
				} else {
					fmt.Printf("\033[33m[WARN]\033[0m Ревью вернуло невалидный результат, используется оригинал.\n")
				}
			} else {
				fmt.Printf("\033[33m[WARN]\033[0m Ревью не удалось: %v\n", revErr)
			}
		}

		fmt.Printf("\n\033[32m[OK]\033[0m Готово за %.1f сек.\n", time.Since(start).Seconds())

		switch {
		case *outFlag != "":
			if err := saveResult(*outFlag, content); err != nil {
				fmt.Printf("\033[31m[ERR]\033[0m Не удалось сохранить: %v\n", err)
			} else {
				fmt.Printf("\033[32m[SAVE]\033[0m %s\n", *outFlag)
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
