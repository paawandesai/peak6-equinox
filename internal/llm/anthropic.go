package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"

// AnthropicClient is a Model backed by the Anthropic Messages API using stdlib net/http.
type AnthropicClient struct {
	apiKey      string
	model       string
	maxTokens   int
	temperature float64
	httpClient  *http.Client
}

// NewAnthropicClient creates a client for the given model ID. apiKey must not
// be empty. maxOutputTokens caps the response length (default 400 per M3-06).
func NewAnthropicClient(apiKey, model string, maxOutputTokens int) (*AnthropicClient, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic api key is required")
	}
	if model == "" {
		return nil, errors.New("model id is required")
	}
	if maxOutputTokens <= 0 {
		maxOutputTokens = 400
	}
	return &AnthropicClient{
		apiKey:      apiKey,
		model:       model,
		maxTokens:   maxOutputTokens,
		temperature: 0.0, // Deterministic, no sampling
		httpClient:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// anthropicRequest is the JSON request body for the Messages API.
type anthropicRequest struct {
	Model       string         `json:"model"`
	MaxTokens   int            `json:"max_tokens"`
	Temperature float64        `json:"temperature"`
	System      string         `json:"system"`
	Messages    []anthropicMsg `json:"messages"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the JSON response from the Messages API.
type anthropicResponse struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
	Model   string             `json:"model"`
	Error   *anthropicError    `json:"error,omitempty"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Complete sends the prompt to the Anthropic API and returns the raw text
// response. It returns an error on transport failure, API error, or empty
// content. The response text is untrusted until Decode accepts it.
func (c *AnthropicClient) Complete(ctx context.Context, p Prompt) (string, error) {
	reqBody := anthropicRequest{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
		System:      p.System,
		Messages:    []anthropicMsg{{Role: "user", Content: p.User}},
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicAPIURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("creating anthropic request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB cap
	if err != nil {
		return "", fmt.Errorf("reading anthropic response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr anthropicResponse
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error != nil {
			return "", fmt.Errorf("anthropic api error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return "", fmt.Errorf("anthropic api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("unmarshaling anthropic response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return "", errors.New("anthropic returned empty content")
	}

	// The verifier prompt asks for a single JSON object, so we concatenate all
	// text blocks. In practice, the model returns exactly one block.
	var text string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	if text == "" {
		return "", errors.New("anthropic returned no text content")
	}

	return text, nil
}
