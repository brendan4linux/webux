// Package ai provides the AI assistant backend.
// Ollama is the primary provider (self-hosted, no API key needed).
// Any OpenAI-compatible endpoint is also supported as fallback.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider identifies the AI backend.
type Provider string

const (
	ProviderOllama     Provider = "ollama"
	ProviderOpenAI     Provider = "openai"
	ProviderAnthropic  Provider = "anthropic"
	ProviderCustom     Provider = "custom"
)

// Config holds all settings for the AI assistant.
type Config struct {
	Provider    Provider `json:"provider"`
	OllamaURL   string   `json:"ollama_url"`   // e.g. http://localhost:11434
	OllamaModel string   `json:"ollama_model"` // e.g. llama3.2:3b
	APIKey      string   `json:"api_key"`      // for cloud providers
	BaseURL     string   `json:"base_url"`     // custom endpoint
	Model       string   `json:"model"`        // for cloud providers
	SystemPrompt string  `json:"system_prompt"`
}

// Message is a single chat turn.
type Message struct {
	Role    string `json:"role"`    // system | user | assistant
	Content string `json:"content"`
}

// OllamaModel is a model returned by Ollama's /api/tags.
type OllamaModel struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
	Details    struct {
		Family     string `json:"family"`
		ParamCount string `json:"parameter_size"`
		Quantize   string `json:"quantization_level"`
	} `json:"details"`
}

// Client is the AI assistant backend.
type Client struct {
	cfg    Config
	http   *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

// ── Ollama management ─────────────────────────────────────────────────────

// OllamaPing returns true if Ollama is reachable at the configured URL.
func (c *Client) OllamaPing() bool {
	url := c.cfg.OllamaURL
	if url == "" {
		url = "http://localhost:11434"
	}
	resp, err := c.http.Get(url + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// OllamaModels returns locally available models.
func (c *Client) OllamaModels() ([]OllamaModel, error) {
	url := c.ollamaURL() + "/api/tags"
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []OllamaModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Models, nil
}

// OllamaPull pulls a model from the Ollama registry.
// Streams progress lines to out channel.
func (c *Client) OllamaPull(ctx context.Context, model string, out chan<- string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"name":   model,
		"stream": true,
	})
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.ollamaURL()+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ollama pull: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &msg) == nil {
			status := fmt.Sprintf("%v", msg["status"])
			if completed, ok := msg["completed"].(float64); ok {
				if total, ok := msg["total"].(float64); ok && total > 0 {
					pct := int(completed / total * 100)
					status = fmt.Sprintf("%s %d%%", status, pct)
				}
			}
			select {
			case out <- status:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return scanner.Err()
}

// OllamaDelete removes a model from local storage.
func (c *Client) OllamaDelete(model string) error {
	body, _ := json.Marshal(map[string]string{"name": model})
	req, err := http.NewRequest("DELETE", c.ollamaURL()+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ── Chat ──────────────────────────────────────────────────────────────────

// Chat sends messages and streams the response token by token.
// Each token is sent to the out channel as it arrives.
func (c *Client) Chat(ctx context.Context, messages []Message, out chan<- string) error {
	switch c.cfg.Provider {
	case ProviderOllama, "":
		return c.chatOllama(ctx, messages, out)
	case ProviderOpenAI, ProviderCustom:
		return c.chatOpenAI(ctx, messages, out)
	case ProviderAnthropic:
		return c.chatAnthropic(ctx, messages, out)
	default:
		return c.chatOllama(ctx, messages, out)
	}
}

// chatOllama uses Ollama's /api/chat endpoint with streaming.
func (c *Client) chatOllama(ctx context.Context, messages []Message, out chan<- string) error {
	model := c.cfg.OllamaModel
	if model == "" {
		// Auto-select first available model
		models, err := c.OllamaModels()
		if err != nil || len(models) == 0 {
			return fmt.Errorf("no Ollama model available — pull one first")
		}
		model = models[0].Name
	}

	// Prepend system message
	allMsgs := []Message{}
	if c.cfg.SystemPrompt != "" {
		allMsgs = append(allMsgs, Message{Role: "system", Content: c.cfg.SystemPrompt})
	}
	allMsgs = append(allMsgs, messages...)

	body, _ := json.Marshal(map[string]interface{}{
		"model":    model,
		"messages": allMsgs,
		"stream":   true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.ollamaURL()+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if json.Unmarshal(scanner.Bytes(), &chunk) == nil {
			if chunk.Message.Content != "" {
				select {
				case out <- chunk.Message.Content:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
	return scanner.Err()
}

// chatOpenAI uses the OpenAI chat completions API (or any compatible endpoint).
func (c *Client) chatOpenAI(ctx context.Context, messages []Message, out chan<- string) error {
	baseURL := c.cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	model := c.cfg.Model
	if model == "" {
		model = "gpt-4o-mini"
	}

	allMsgs := []Message{}
	if c.cfg.SystemPrompt != "" {
		allMsgs = append(allMsgs, Message{Role: "system", Content: c.cfg.SystemPrompt})
	}
	allMsgs = append(allMsgs, messages...)

	body, _ := json.Marshal(map[string]interface{}{
		"model":    model,
		"messages": allMsgs,
		"stream":   true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST",
		baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("openai chat: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) == nil &&
			len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			select {
			case out <- chunk.Choices[0].Delta.Content:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return scanner.Err()
}

// chatAnthropic uses the Anthropic messages API with streaming.
func (c *Client) chatAnthropic(ctx context.Context, messages []Message, out chan<- string) error {
	// Filter out system messages — Anthropic takes system separately
	var systemMsg string
	var userMsgs []Message
	for _, m := range messages {
		if m.Role == "system" {
			systemMsg = m.Content
		} else {
			userMsgs = append(userMsgs, m)
		}
	}
	if systemMsg == "" {
		systemMsg = c.cfg.SystemPrompt
	}

	model := c.cfg.Model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	payload := map[string]interface{}{
		"model":      model,
		"max_tokens": 2048,
		"messages":   userMsgs,
		"stream":     true,
	}
	if systemMsg != "" {
		payload["system"] = systemMsg
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic chat: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		data := strings.TrimPrefix(line, "data: ")
		if json.Unmarshal([]byte(data), &event) == nil &&
			event.Type == "content_block_delta" &&
			event.Delta.Text != "" {
			select {
			case out <- event.Delta.Text:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return scanner.Err()
}

func (c *Client) ollamaURL() string {
	if c.cfg.OllamaURL != "" {
		return strings.TrimRight(c.cfg.OllamaURL, "/")
	}
	return "http://localhost:11434"
}

// RecommendedModels returns curated model suggestions with RAM requirements.
func RecommendedModels() []struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	RAM         string `json:"ram"`
	Good        string `json:"good_for"`
} {
	return []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		RAM         string `json:"ram"`
		Good        string `json:"good_for"`
	}{
		{"llama3.2:3b",    "Llama 3.2 3B — fast and lightweight",      "4 GB",  "Quick answers, low-RAM servers"},
		{"llama3.2:1b",    "Llama 3.2 1B — very fast, minimal RAM",     "2 GB",  "Minimal hardware, quick responses"},
		{"qwen2.5:7b",     "Qwen 2.5 7B — strong reasoning",            "8 GB",  "Better analysis and code help"},
		{"qwen2.5:14b",    "Qwen 2.5 14B — high quality",               "16 GB", "Complex troubleshooting"},
		{"mistral:7b",     "Mistral 7B — well-rounded",                  "8 GB",  "General sysadmin tasks"},
		{"phi4:14b",       "Phi-4 14B — Microsoft research model",       "16 GB", "Technical depth"},
		{"gemma3:4b",      "Gemma 3 4B — Google, efficient",             "6 GB",  "Balanced performance"},
	}
}
