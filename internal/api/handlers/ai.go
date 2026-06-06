package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	sysai "github.com/brendan4linux/webux/internal/system/ai"
)

type AIHandler struct {
	db *sql.DB
}

func NewAIHandler(db *sql.DB) *AIHandler {
	return &AIHandler{db: db}
}

func (h *AIHandler) getSetting(key, def string) string {
	var val string
	if err := h.db.QueryRow("SELECT value FROM webux_settings WHERE key = ?", key).Scan(&val); err != nil || val == "" {
		return def
	}
	return val
}

func (h *AIHandler) setSetting(key, val string) {
	h.db.Exec(`INSERT INTO webux_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=datetime('now')`, key, val)
}

func (h *AIHandler) buildClient() *sysai.Client {
	return sysai.NewClient(sysai.Config{
		Provider:     sysai.Provider(h.getSetting("ai.provider", "ollama")),
		OllamaURL:    h.getSetting("ai.ollama_url", "http://localhost:11434"),
		OllamaModel:  h.getSetting("ai.ollama_model", ""),
		APIKey:       h.getSetting("ai.api_key", ""),
		BaseURL:      h.getSetting("ai.base_url", ""),
		Model:        h.getSetting("ai.model", ""),
		SystemPrompt: h.getSetting("ai.system_prompt", ""),
	})
}

// Status handles GET /api/ai/status
func (h *AIHandler) Status(w http.ResponseWriter, r *http.Request) {
	client := h.buildClient()
	ollamaPing := client.OllamaPing()

	var models []sysai.OllamaModel
	if ollamaPing {
		models, _ = client.OllamaModels()
	}

	writeJSON(w, map[string]interface{}{
		"provider":      h.getSetting("ai.provider", "ollama"),
		"ollama_url":    h.getSetting("ai.ollama_url", "http://localhost:11434"),
		"ollama_model":  h.getSetting("ai.ollama_model", ""),
		"ollama_online": ollamaPing,
		"local_models":  models,
		"recommended":   sysai.RecommendedModels(),
		"has_api_key":   h.getSetting("ai.api_key", "") != "",
		"system_prompt": h.getSetting("ai.system_prompt", ""),
	})
}

// SaveSettings handles PUT /api/ai/settings
func (h *AIHandler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider     string `json:"provider"`
		OllamaURL    string `json:"ollama_url"`
		OllamaModel  string `json:"ollama_model"`
		APIKey       string `json:"api_key"`
		BaseURL      string `json:"base_url"`
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Provider != ""     { h.setSetting("ai.provider", body.Provider) }
	if body.OllamaURL != ""    { h.setSetting("ai.ollama_url", body.OllamaURL) }
	h.setSetting("ai.ollama_model", body.OllamaModel) // allow empty to mean "auto"
	if body.APIKey != ""       { h.setSetting("ai.api_key", body.APIKey) }
	if body.BaseURL != ""      { h.setSetting("ai.base_url", body.BaseURL) }
	if body.Model != ""        { h.setSetting("ai.model", body.Model) }
	if body.SystemPrompt != "" { h.setSetting("ai.system_prompt", body.SystemPrompt) }
	writeJSON(w, map[string]interface{}{"ok": true})
}

// ListModels handles GET /api/ai/models — returns Ollama local models
func (h *AIHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	client := h.buildClient()
	models, err := client.OllamaModels()
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"models": []interface{}{},
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, map[string]interface{}{
		"models":      models,
		"recommended": sysai.RecommendedModels(),
	})
}

// PullModel handles POST /api/ai/models/pull — streams SSE download progress
func (h *AIHandler) PullModel(w http.ResponseWriter, r *http.Request) {
	var body struct{ Model string `json:"model"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)

	client := h.buildClient()
	out := make(chan string, 32)

	go func() {
		err := client.OllamaPull(r.Context(), body.Model, out)
		if err != nil {
			out <- fmt.Sprintf("[error] %s", err.Error())
		} else {
			out <- "[done] " + body.Model + " ready"
			// Auto-select this model if none set
			if h.getSetting("ai.ollama_model", "") == "" {
				h.setSetting("ai.ollama_model", body.Model)
			}
		}
		close(out)
	}()

	for line := range out {
		fmt.Fprintf(w, "data: %s\n\n", line)
		if ok {
			flusher.Flush()
		}
	}
}

// DeleteModel handles DELETE /api/ai/models
func (h *AIHandler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	var body struct{ Model string `json:"model"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}
	client := h.buildClient()
	if err := client.OllamaDelete(body.Model); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

// Chat handles POST /api/ai/chat — streams SSE token-by-token response
func (h *AIHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []sysai.Message `json:"messages"`
		Context  string          `json:"context"` // current page context injected by frontend
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Inject page context into the last user message if provided
	if body.Context != "" && len(body.Messages) > 0 {
		last := &body.Messages[len(body.Messages)-1]
		if last.Role == "user" {
			last.Content = last.Content + "\n\n[System context]\n" + body.Context
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)

	client := h.buildClient()
	out := make(chan string, 128)

	go func() {
		err := client.Chat(r.Context(), body.Messages, out)
		if err != nil {
			out <- "\n\n[error: " + err.Error() + "]"
		}
		close(out)
	}()

	for token := range out {
		// Escape newlines for SSE — use JSON encoding
		data, _ := json.Marshal(token)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if ok {
			flusher.Flush()
		}
	}
	// Signal completion
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if ok {
		flusher.Flush()
	}
}
