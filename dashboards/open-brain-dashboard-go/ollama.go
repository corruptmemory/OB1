package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaClient talks to Ollama's OpenAI-compatible endpoint at /v1/embeddings
// for vector search and the native /api/chat endpoint for metadata extraction.
type OllamaClient struct {
	baseURL   string
	model     string // embedding model (e.g. mxbai-embed-large)
	chatModel string // chat model for extraction (e.g. qwen2.5:3b)
	hc        *http.Client
}

func NewOllamaClient(baseURL, model, chatModel string) *OllamaClient {
	return &OllamaClient{
		baseURL:   baseURL,
		model:     model,
		chatModel: chatModel,
		hc:        &http.Client{Timeout: 30 * time.Second},
	}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed turns a single input string into an embedding vector.
func (c *OllamaClient) Embed(ctx context.Context, input string) ([]float32, error) {
	payload, err := json.Marshal(embedRequest{Model: c.model, Input: input})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama status %d: %s", resp.StatusCode, string(body))
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("ollama returned no embeddings")
	}
	return out.Data[0].Embedding, nil
}

// ExtractedMetadata holds the AI-extracted fields from a thought's
// content text, mirroring the MCP server's OpenRouter extraction shape.
type ExtractedMetadata struct {
	Type           string   `json:"type"`
	Topics         []string `json:"topics"`
	People         []string `json:"people"`
	ActionItems    []string `json:"action_items"`
	DatesMentioned []string `json:"dates_mentioned"`
}

const extractionPrompt = `Extract metadata from the user's captured thought. Return JSON with:
- "people": array of people mentioned (empty if none)
- "action_items": array of implied to-dos (empty if none)
- "dates_mentioned": array of dates YYYY-MM-DD (empty if none)
- "topics": array of 1-3 short topic tags (always at least one)
- "type": one of "observation", "task", "idea", "reference", "person_note"
Only extract what's explicitly there.`

// Extract calls the Ollama chat API to pull structured metadata out of
// a thought's content text. Returns a best-effort result — on any
// failure (model down, malformed JSON, timeout) it returns a zero-value
// ExtractedMetadata so the caller can proceed with user-entered values.
func (c *OllamaClient) Extract(ctx context.Context, content string) ExtractedMetadata {
	if c.chatModel == "" {
		return ExtractedMetadata{}
	}

	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Format   string        `json:"format"`
		Stream   bool          `json:"stream"`
	}{
		Model: c.chatModel,
		Messages: []chatMessage{
			{Role: "system", Content: extractionPrompt},
			{Role: "user", Content: content},
		},
		Format: "json",
		Stream: false,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ExtractedMetadata{}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return ExtractedMetadata{}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return ExtractedMetadata{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ExtractedMetadata{}
	}

	var chatResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return ExtractedMetadata{}
	}

	var meta ExtractedMetadata
	if err := json.Unmarshal([]byte(chatResp.Message.Content), &meta); err != nil {
		return ExtractedMetadata{}
	}
	return meta
}

const synthesizePrompt = `You are a note consolidation assistant. You will receive multiple notes that overlap in topic. Combine them into a single concise note that preserves all unique information, removes redundancy, and reads naturally. Output only the merged note text, nothing else.`

// Synthesize calls the Ollama chat API to merge multiple thought contents
// into one consolidated note. Returns the merged text. On any failure
// (model down, timeout, etc.) returns an error so the caller can fall
// back to raw concatenation.
func (c *OllamaClient) Synthesize(ctx context.Context, contents []string) (string, error) {
	if c.chatModel == "" {
		return "", fmt.Errorf("no chat model configured")
	}

	// Build a user message with each thought numbered.
	var buf bytes.Buffer
	for i, content := range contents {
		fmt.Fprintf(&buf, "--- Note %d ---\n%s\n\n", i+1, content)
	}

	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}{
		Model: c.chatModel,
		Messages: []chatMessage{
			{Role: "system", Content: synthesizePrompt},
			{Role: "user", Content: buf.String()},
		},
		Stream: false,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal synthesize request: %w", err)
	}

	// Synthesis can take a while on CPU-only Ollama with multiple long
	// thoughts, so use a generous timeout independent of the client default.
	synthCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(synthCtx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create synthesize request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama synthesize: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama synthesize status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decode synthesize response: %w", err)
	}

	return chatResp.Message.Content, nil
}
