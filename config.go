package main

import (
	"encoding/json"
	"os"
)

// Config — настройки приложения из config.json.
type Config struct {
	Provider     string `json:"provider"`
	GeminiAPIKey string `json:"gemini_api_key"`
	GeminiModel  string `json:"gemini_model"`
	OllamaURL    string `json:"ollama_url"`
	OllamaModel  string `json:"ollama_model"`
	ClaudeAPIKey string `json:"claude_api_key"`
	ClaudeModel  string `json:"claude_model"`
	// SystemPrompt переопределяет встроенный промпт. Поддерживает плейсхолдер {{context}}.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Context подставляется в {{context}} системного промпта.
	Context string `json:"context,omitempty"`
	// ReviewMode включает второй LLM-проход ревью после генерации.
	ReviewMode bool `json:"review_mode,omitempty"`
	// MaxRetries — число повторов при ошибке валидации (дефолт: 2).
	MaxRetries int `json:"max_retries,omitempty"`
}

// defaultMaxRetries — используется когда max_retries не задан в конфиге.
const defaultMaxRetries = 2

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config Config
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}
