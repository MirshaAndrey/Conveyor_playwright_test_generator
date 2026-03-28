package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ─── Структуры API ────────────────────────────────────────────────────────────

type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	System string `json:"system"`
	Stream bool   `json:"stream"`
}

type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

type OllamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ─── Системный промпт ────────────────────────────────────────────────────────

const systemPrompt = `Ты — senior QA Automation инженер. Генерируй production-ready Playwright тесты. Требования: TypeScript, Playwright Test, test.describe + test, переиспользуемый код. Селекторы: используй getByTestId (предпочтительно), getByRole (с name), getByLabel; избегай XPath, nth-child и нестабильных селекторов. Логика: каждый шаг чеклиста → действие + проверка; после каждого действия добавляй expect; проверяй UI состояние (visible, enabled, text). Умные допущения: если нет селекторов — генерируй логичные data-testid (login-button, submit-form) и имитируй реальный пользовательский сценарий. Анти-паттерны (запрещено): waitForTimeout, console.log, пустые тесты, TODO, невалидный код. Качество: код должен быть сразу исполняемым, без синтаксических ошибок и без «примеров» — только реальный код. Контекст страницы: {{context}} Чеклист: {{input}}`

// ─── Подключение и модели ─────────────────────────────────────────────────────

func checkOllama(baseURL string) error {
	resp, err := http.Get(baseURL)
	if err != nil {
		return fmt.Errorf("не удалось подключиться: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("неожиданный статус: %s", resp.Status)
	}
	return nil
}

func fetchModels(baseURL string) ([]string, error) {
	resp, err := http.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tags OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

func pickModel(reader *bufio.Reader, models []string, defaultModel string) string {
	fmt.Println("\nДоступные модели:")
	for i, m := range models {
		marker := " "
		if m == defaultModel {
			marker = "★"
		}
		fmt.Printf("  %s %d. %s\n", marker, i+1, m)
	}
	fmt.Printf("\nВведи номер модели [по умолчанию: %s]: ", defaultModel)

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultModel
	}
	var idx int
	if _, err := fmt.Sscanf(input, "%d", &idx); err == nil {
		if idx >= 1 && idx <= len(models) {
			return models[idx-1]
		}
		fmt.Println("⚠️  Неверный номер, используется модель по умолчанию.")
	}
	return defaultModel
}

// ─── Генерация ────────────────────────────────────────────────────────────────

// sendRequest отправляет запрос к Ollama и читает стриминговый ответ.
// onChunk вызывается для каждого полученного токена (nil — для тихого режима).
func sendRequest(baseURL, model, taskText string, onChunk func(string)) (string, error) {
	reqData := OllamaRequest{
		Model:  model,
		Prompt: taskText,
		System: systemPrompt,
		Stream: true,
	}
	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return "", fmt.Errorf("ошибка формирования JSON: %v", err)
	}

	resp, err := http.Post(baseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("ошибка запроса к Ollama: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama вернула статус: %s", resp.Status)
	}

	var result strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var chunk OllamaResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		if onChunk != nil {
			onChunk(chunk.Response)
		}
		result.WriteString(chunk.Response)
		if chunk.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("ошибка чтения потока: %v", err)
	}

	return result.String(), nil
}

// generate — тихая генерация (для пакетного режима)
func generate(baseURL, model, taskText string) (string, error) {
	return sendRequest(baseURL, model, taskText, nil)
}

// generateWithStream — генерация со стримингом токенов в stdout
func generateWithStream(baseURL, model, taskText string) (string, error) {
	return sendRequest(baseURL, model, taskText, func(token string) {
		fmt.Print(token)
	})
}
