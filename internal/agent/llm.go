package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// LLMClient wraps the HTTP calls to GitHub Models API (OpenAI compatible)
type LLMClient struct {
	Token   string
	BaseURL string
	Model   string
}

// NewLLMClient creates a client pointing to GitHub Models
func NewLLMClient(token, model string) *LLMClient {
	if model == "" {
		model = "gpt-4.1" // Default model, extremely fast and cost-efficient
	}
	return &LLMClient{
		Token:   token,
		BaseURL: "https://models.inference.ai.azure.com",
		Model:   model,
	}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ResponseFormat struct {
	Type       string      `json:"type,omitempty"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

type JSONSchema struct {
	Name   string      `json:"name"`
	Strict bool        `json:"strict"`
	Schema interface{} `json:"schema"`
}

type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// GenerateContent sends a chat request to GitHub Models and returns the string response
func (c *LLMClient) GenerateContent(ctx context.Context, systemInstruction, prompt string, jsonSchema *JSONSchema) (string, error) {
	reqBody := ChatRequest{
		Model: c.Model,
		Messages: []Message{
			{Role: "system", Content: systemInstruction},
			{Role: "user", Content: prompt},
		},
	}

	if jsonSchema != nil {
		reqBody.ResponseFormat = &ResponseFormat{
			Type:       "json_schema",
			JSONSchema: jsonSchema,
		}
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respData))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respData, &chatResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w, raw: %s", err, string(respData))
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned by model")
	}

	return chatResp.Choices[0].Message.Content, nil
}
