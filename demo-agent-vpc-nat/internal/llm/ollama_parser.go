package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RawIntent represents the unvalidated parameters extracted from the user prompt.
type RawIntent struct {
	StackName    string `json:"stack_name"`
	VpcCIDR      string `json:"vpc_cidr"`
	InstanceType string `json:"instance_type"`
	AllowedPorts []int  `json:"allowed_ports"`
	PublicAccess bool   `json:"public_access"`
	EnableNatGateway bool  `json:"enable_nat_gateway"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
}

var intentJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "stack_name": {
      "type": "string",
      "description": "Short lowercase alphanumeric name for the deployment stack"
    },
    "vpc_cidr": {
      "type": "string",
      "description": "IPv4 CIDR block for the VPC, e.g., 10.10.0.0/16"
    },
    "instance_type": {
      "type": "string",
      "description": "AWS EC2 instance type requested by user, e.g., t3.micro"
    },
    "allowed_ports": {
      "type": "array",
      "items": { "type": "integer" },
      "description": "List of TCP ports requested to open"
    },
    "public_access": {
      "type": "boolean",
      "description": "Whether the user requested direct internet exposure"
    },
    "enable_nat_gateway":{
      "type": "boolean",
      "description": "True if the user explicitely requested a NAT gateway"
    }
  },
  "required": ["stack_name", "vpc_cidr", "instance_type", "allowed_ports", "public_access", "enable_nat_gateway"],
  "additionalProperties": false
}`)

const systemPrompt = `You are an infrastructure intent extractor. 
Extract cloud deployment parameters from the user's natural language request into the required JSON schema.
Rules:
- If no VPC CIDR is mentioned, default to "10.10.0.0/16".
- ONLY populate instance_type if the user explicitly asks for a server, VM, or EC2 instance; otherwise set it to "".
- Set enable_nat_gateway to true ONLY if the user explicitly asks for a NAT gateway.
- If no stack name is mentioned, generate a short lowercase name like "dev-vpc".
- Extract any explicitly requested ports (e.g., SSH = 22, HTTP = 80, HTTPS = 443) even if they might be unsafe; downstream security guardrails will evaluate them.`

// ParsePromptToIntent sends a natural language prompt to local Ollama and returns a RawIntent struct.
func ParsePromptToIntent(ctx context.Context, ollamaURL, model, userPrompt string) (RawIntent, error) {
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	if model == "" {
		model = "qwen2.5:1.5b"
	}

	reqBody := ollamaChatRequest{
		Model:  model,
		Stream: false,
		Format: intentJSONSchema,
		Options: map[string]any{
			"temperature": 0.0,
		},
		Messages: []ollamaMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return RawIntent{}, fmt.Errorf("failed to marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return RawIntent{}, fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return RawIntent{}, fmt.Errorf("ollama request failed (is ollama running?): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return RawIntent{}, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return RawIntent{}, fmt.Errorf("failed to decode ollama response envelope: %w", err)
	}

	var intent RawIntent
	decoder := json.NewDecoder(strings.NewReader(chatResp.Message.Content))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&intent); err != nil {
		return RawIntent{}, fmt.Errorf("failed to parse structured intent JSON: %w", err)
	}

	return intent, nil
}


