package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ─── Структуры Gemini API ─────────────────────────────────────────────────────

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiRequest struct {
	SystemInstruction *GeminiContent  `json:"systemInstruction,omitempty"`
	Contents          []GeminiContent `json:"contents"`
}

type GeminiCandidate struct {
	Content struct {
		Parts []GeminiPart `json:"parts"`
	} `json:"content"`
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
}

// ─── Базовая функция запроса ──────────────────────────────────────────────────

// sendGeminiRequest отправляет запрос к Gemini API.
// sysTpl — системный промпт; stream — использовать SSE; onChunk — коллбек на токен.
func sendGeminiRequest(apiKey, model, prompt, sysTpl string, stream bool, onChunk func(string)) (string, error) {
	reqData := GeminiRequest{
		SystemInstruction: &GeminiContent{
			Role:  "system",
			Parts: []GeminiPart{{Text: sysTpl}},
		},
		Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: prompt}}},
		},
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return "", fmt.Errorf("ошибка формирования JSON для Gemini: %v", err)
	}

	base := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s", model)
	var endpoint string
	if stream {
		endpoint = base + ":streamGenerateContent?alt=sse&key=" + apiKey
	} else {
		endpoint = base + ":generateContent?key=" + apiKey
	}

	resp, err := http.Post(endpoint, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("ошибка запроса к Gemini API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errData)
		return "", fmt.Errorf("gemini API вернул статус %s: %v", resp.Status, errData)
	}

	var result strings.Builder

	if stream {
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
			var chunk GeminiResponse
			if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
				if len(chunk.Candidates) > 0 && len(chunk.Candidates[0].Content.Parts) > 0 {
					text := chunk.Candidates[0].Content.Parts[0].Text
					if onChunk != nil {
						onChunk(text)
					}
					result.WriteString(text)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("ошибка чтения потока Gemini: %v", err)
		}
	} else {
		var respData GeminiResponse
		if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
			return "", fmt.Errorf("ошибка декодирования ответа Gemini: %v", err)
		}
		if len(respData.Candidates) == 0 || len(respData.Candidates[0].Content.Parts) == 0 {
			return "", fmt.Errorf("пустой ответ от Gemini API")
		}
		text := respData.Candidates[0].Content.Parts[0].Text
		if onChunk != nil {
			onChunk(text)
		}
		result.WriteString(text)
	}

	return result.String(), nil
}

// geminiGenerateWithStream — генерация со стримингом в stdout (интерактивный режим).
func geminiGenerateWithStream(apiKey, model, prompt, sysTpl string) (string, error) {
	return sendGeminiRequest(apiKey, model, prompt, sysTpl, true, func(token string) {
		fmt.Print(token)
	})
}
