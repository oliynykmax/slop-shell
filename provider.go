package main

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

// ChatRequest is the wire-format request for any OpenAI-compatible provider.
type ChatRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	// Thinking disables extended reasoning / chain-of-thought. DeepSeek V4
	// defaults to thinking mode; pass Type="disabled" to skip it. On providers
	// that don't support the parameter the field is omitted (omitempty).
	Thinking *ThinkingOptions `json:"thinking,omitempty"`
}

// ThinkingOptions toggles chain-of-thought on V4-era models. Type must be
// "enabled" or "disabled".
type ThinkingOptions struct {
	Type string `json:"type"`
}

// Provider abstracts an OpenAI-compatible chat backend.
type Provider interface {
	Name() string
	Models() []string
	Chat(ctx context.Context, req ChatRequest) (string, error)
	Stream(ctx context.Context, req ChatRequest, onDelta func(delta string)) (string, error)
	Probe(ctx context.Context, model string) error
}

// --- DeepSeek ---

type DeepSeek struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewDeepSeek(apiKey string) *DeepSeek {
	return &DeepSeek{
		apiKey:  apiKey,
		baseURL: "https://api.deepseek.com/v1",
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (d *DeepSeek) Name() string  { return "deepseek" }
func (d *DeepSeek) Models() []string {
	return []string{"deepseek-v4-flash"}
}

func (d *DeepSeek) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	return req, nil
}

func (d *DeepSeek) doWithRetry(ctx context.Context, body []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := d.newRequest(ctx, body)
		if err != nil {
			return nil, err
		}
		resp, err := d.client.Do(req)
		if err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<uint(attempt)) * time.Second):
			}
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<uint(attempt)) * time.Second):
			}
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

func (d *DeepSeek) parseErrorBody(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var apiErr struct {
		Error *APIError `json:"error"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != nil {
		return fmt.Errorf("API error: %s", apiErr.Error.Message)
	}
	return fmt.Errorf("API error: HTTP %d", resp.StatusCode)
}

func (d *DeepSeek) Chat(ctx context.Context, req ChatRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	resp, err := d.doWithRetry(ctx, body)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", d.parseErrorBody(resp)
	}

	var dsResp OpenAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	if dsResp.Error != nil {
		return "", fmt.Errorf("API error: %s", dsResp.Error.Message)
	}
	if len(dsResp.Choices) == 0 {
		return "", fmt.Errorf("empty response from model")
	}
	return dsResp.Choices[0].Message.Content, nil
}

func (d *DeepSeek) Stream(ctx context.Context, req ChatRequest, onDelta func(string)) (string, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	resp, err := d.doWithRetry(ctx, body)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", d.parseErrorBody(resp)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var fullText strings.Builder
	for scanner.Scan() {
		if ctx.Err() != nil {
			return fullText.String(), ctx.Err()
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk OpenAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			return fullText.String(), fmt.Errorf("stream error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) > 0 {
			text := chunk.Choices[0].Delta.Content
			if text != "" {
				fullText.WriteString(text)
				if onDelta != nil {
					onDelta(text)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fullText.String(), fmt.Errorf("stream read: %w", err)
	}
	return fullText.String(), nil
}

func (d *DeepSeek) Probe(ctx context.Context, model string) error {
	req := ChatRequest{
		Model:       model,
		Messages:    []OpenAIMessage{{Role: "user", Content: "echo test"}},
		Temperature: 0,
		MaxTokens:   32,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := d.doWithRetry(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return d.parseErrorBody(resp)
	}
	// Validate the body actually looks like a model reply — a 200 with a
	// broken/missing body would otherwise look like a working model and the
	// real chat call would fail later.
	var dsResp OpenAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return fmt.Errorf("invalid response: %w", err)
	}
	if len(dsResp.Choices) == 0 {
		return fmt.Errorf("empty response (no choices)")
	}
	return nil
}

// selectModel picks the first model in the provider's list that responds to a probe.
func selectModel(ctx context.Context, p Provider) (string, error) {
	var lastErr error
	for _, model := range p.Models() {
		if err := p.Probe(ctx, model); err == nil {
			return model, nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("no working %s model (last error: %v)", p.Name(), lastErr)
	}
	return "", fmt.Errorf("no working %s model — check your API key", p.Name())
}
