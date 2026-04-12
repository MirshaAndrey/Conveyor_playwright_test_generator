package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const claudeAPIURL = "https://api.anthropic.com/v1/messages"
const claudeAPIVersion = "2023-06-01"

// Список доступных моделей Claude (для интерактивного выбора).
var claudeModels = []string{
	"claude-sonnet-4-6",
	"claude-opus-4-6",
	"claude-haiku-4-5-20251001",
}

const claudeDefaultModel = "claude-sonnet-4-6"

// ─── Структуры Claude API ─────────────────────────────────────────────────────

type ClaudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ClaudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []ClaudeMessage `json:"messages"`
	Stream    bool            `json:"stream,omitempty"`
}

type ClaudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ClaudeResponse struct {
	Content []ClaudeContentBlock `json:"content"`
}

// ClaudeStreamEvent — SSE-событие от Anthropic API.
type ClaudeStreamEvent struct {
	Type  string          `json:"type"`
	Delta ClaudeTextDelta `json:"delta"`
}

type ClaudeTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ─── Базовая функция запроса ──────────────────────────────────────────────────

// sendClaudeRequest отправляет запрос к Anthropic API.
// stream — использовать SSE-стриминг; onChunk — коллбек на каждый токен (nil = тихо).
func sendClaudeRequest(apiKey, model, prompt, systemPrompt string, stream bool, onChunk func(string)) (string, error) {
	reqData := ClaudeRequest{
		Model:     model,
		MaxTokens: 8192,
		System:    systemPrompt,
		Messages:  []ClaudeMessage{{Role: "user", Content: prompt}},
		Stream:    stream,
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return "", fmt.Errorf("ошибка формирования JSON для Claude: %v", err)
	}

	req, err := http.NewRequest("POST", claudeAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса к Claude API: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка запроса к Claude API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errData)
		return "", fmt.Errorf("Claude API вернул статус %s: %v", resp.Status, errData)
	}

	var result strings.Builder

	if stream {
		// Anthropic SSE: строки вида "event: ..." и "data: {...}"
		// Нас интересуют только data-строки с type="content_block_delta".
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			dataStr := strings.TrimPrefix(line, "data: ")
			if dataStr == "[DONE]" {
				break
			}
			var event ClaudeStreamEvent
			if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
				continue
			}
			if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
				text := event.Delta.Text
				if onChunk != nil {
					onChunk(text)
				}
				result.WriteString(text)
			}
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("ошибка чтения потока Claude: %v", err)
		}
	} else {
		var respData ClaudeResponse
		if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
			return "", fmt.Errorf("ошибка декодирования ответа Claude: %v", err)
		}
		if len(respData.Content) == 0 {
			return "", fmt.Errorf("пустой ответ от Claude API")
		}
		text := respData.Content[0].Text
		if onChunk != nil {
			onChunk(text)
		}
		result.WriteString(text)
	}

	return result.String(), nil
}

// claudeGenerateWithStream — генерация со стримингом в stdout (интерактивный режим).
func claudeGenerateWithStream(apiKey, model, prompt, sysTpl string) (string, error) {
	return sendClaudeRequest(apiKey, model, prompt, sysTpl, true, func(token string) {
		fmt.Print(token)
	})
}
