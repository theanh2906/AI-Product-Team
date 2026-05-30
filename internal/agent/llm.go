package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiClient wraps HTTP calls to the Gemini API.
type GeminiClient struct {
	APIKey string
	Model  string
}

// NewGeminiClient creates a new Gemini client. Defaults to gemini-2.5-flash if model is empty.
func NewGeminiClient(apiKey, model string) *GeminiClient {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &GeminiClient{APIKey: apiKey, Model: model}
}

// --- Gemini API types ---

// Part is a single unit of content: text, a function call, or a function response.
type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

// FunctionCall represents a tool invocation requested by the model.
type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// FunctionResponse carries the result of a tool call back to the model.
type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// Content is a turn in the conversation (user, model, or system).
type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

// FunctionDeclaration describes a callable tool to the model.
type FunctionDeclaration struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// GeminiTool groups function declarations into a single tool.
type GeminiTool struct {
	FunctionDeclarations []FunctionDeclaration `json:"function_declarations"`
}

// GenerationConfig controls output format and schema.
type GenerationConfig struct {
	ResponseMimeType string      `json:"responseMimeType,omitempty"`
	ResponseSchema   interface{} `json:"responseSchema,omitempty"`
}

// JSONSchema is kept for callers that pass structured output constraints.
type JSONSchema struct {
	Name   string      `json:"name"`
	Strict bool        `json:"strict"`
	Schema interface{} `json:"schema"`
}

// ToolHandler is called each time Gemini requests a function call.
type ToolHandler func(name string, args map[string]interface{}) (string, error)

// GitHubCodeTools is the standard tool set for reading a GitHub repository.
var GitHubCodeTools = []GeminiTool{
	{
		FunctionDeclarations: []FunctionDeclaration{
			{
				Name:        "list_files",
				Description: "List files and directories at a given path in the GitHub repository. Use empty string for the repository root.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Directory path to list. Use empty string for the repository root.",
						},
					},
					"required": []string{"path"},
				},
			},
			{
				Name:        "get_file_content",
				Description: "Read the full source content of a file from the GitHub repository.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "File path relative to the repository root, e.g. 'src/main.go'",
						},
					},
					"required": []string{"path"},
				},
			},
		},
	},
}

// --- Internal request/response structs ---

type geminiRequest struct {
	SystemInstruction *Content          `json:"system_instruction,omitempty"`
	Contents          []Content         `json:"contents"`
	Tools             []GeminiTool      `json:"tools,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
}

type geminiCandidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

func (c *GeminiClient) post(ctx context.Context, req geminiRequest) (*geminiResponse, error) {
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiBaseURL, c.Model, c.APIKey)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini API request failed with status %d: %s", resp.StatusCode, string(respData))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respData, &gemResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w, raw: %s", err, string(respData))
	}

	if len(gemResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates returned by model")
	}

	return &gemResp, nil
}

// GenerateContent sends a simple request (no tools) and returns the text response.
// Pass a non-nil JSONSchema to request structured JSON output.
func (c *GeminiClient) GenerateContent(ctx context.Context, systemInstruction, prompt string, jsonSchema *JSONSchema) (string, error) {
	req := geminiRequest{
		SystemInstruction: &Content{
			Parts: []Part{{Text: systemInstruction}},
		},
		Contents: []Content{
			{Role: "user", Parts: []Part{{Text: prompt}}},
		},
	}

	if jsonSchema != nil {
		req.GenerationConfig = &GenerationConfig{
			ResponseMimeType: "application/json",
			ResponseSchema:   jsonSchema.Schema,
		}
	}

	resp, err := c.post(ctx, req)
	if err != nil {
		return "", err
	}

	return partsToText(resp.Candidates[0].Content.Parts), nil
}

// GenerateWithTools runs an agentic loop: Gemini calls tools until it produces a final text response.
func (c *GeminiClient) GenerateWithTools(ctx context.Context, systemInstruction, prompt string, tools []GeminiTool, handler ToolHandler) (string, error) {
	req := geminiRequest{
		SystemInstruction: &Content{
			Parts: []Part{{Text: systemInstruction}},
		},
		Contents: []Content{
			{Role: "user", Parts: []Part{{Text: prompt}}},
		},
		Tools: tools,
	}

	for {
		resp, err := c.post(ctx, req)
		if err != nil {
			return "", err
		}

		modelContent := resp.Candidates[0].Content
		modelContent.Role = "model"

		// Collect function calls from this turn
		var fnCalls []Part
		for _, p := range modelContent.Parts {
			if p.FunctionCall != nil {
				fnCalls = append(fnCalls, p)
			}
		}

		// No more tool calls — return the accumulated text
		if len(fnCalls) == 0 {
			return partsToText(modelContent.Parts), nil
		}

		// Append the model's turn to the conversation
		req.Contents = append(req.Contents, modelContent)

		// Execute each tool call and collect responses
		responseParts := make([]Part, 0, len(fnCalls))
		for _, fc := range fnCalls {
			fmt.Printf("  [Gemini tool call]: %s(%v)\n", fc.FunctionCall.Name, fc.FunctionCall.Args)
			result, err := handler(fc.FunctionCall.Name, fc.FunctionCall.Args)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			}
			responseParts = append(responseParts, Part{
				FunctionResponse: &FunctionResponse{
					Name:     fc.FunctionCall.Name,
					Response: map[string]interface{}{"content": result},
				},
			})
		}

		// Append tool results as a user turn
		req.Contents = append(req.Contents, Content{
			Role:  "user",
			Parts: responseParts,
		})
	}
}

func partsToText(parts []Part) string {
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.Text)
	}
	return sb.String()
}
