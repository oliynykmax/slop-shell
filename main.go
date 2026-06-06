package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"
)

// Models to try in order of preference (newest/best first).
var modelCandidates = []string{
	"gemini-2.5-flash",
	"gemini-2.5-pro",
	"gemini-2.0-flash",
	"gemini-1.5-flash",
}

// --- Gemini API types ---

type GeminiRequest struct {
	Contents         []Content        `json:"contents"`
	SystemInstruction *Content        `json:"systemInstruction,omitempty"`
	GenerationConfig *GenerationConfig `json:"generationConfig,omitempty"`
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type GenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type GeminiResponse struct {
	Candidates []Candidate `json:"candidates"`
	Error      *APIError   `json:"error,omitempty"`
}

type Candidate struct {
	Content Content `json:"content"`
}

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// --- Shell state ---

type SlopShell struct {
	apiKey     string
	model      string
	history    []Content
	systemPrompt Content
	client     *http.Client
}

func newSlopShell(apiKey, model string) *SlopShell {
	hostname, _ := os.Hostname()
	user := os.Getenv("USER")
	if user == "" {
		user = "user"
	}

	systemText := fmt.Sprintf(`You are a fully hallucinated Unix/Linux shell. You must pretend to be a real bash shell running on a Linux system. You are NOT an AI assistant — you ARE a shell.

CRITICAL RULES:
1. NEVER use markdown formatting. No backticks, no bold, no headers, no bullet points. Output raw text exactly like a terminal would.
2. NEVER break character. You are a shell. If someone asks "are you AI?" respond like a shell would — command not found or similar.
3. NEVER say "I'm an AI" or "I can't actually execute commands" or anything like that. You ARE the shell.
4. Respond ONLY with what a real terminal would output. Nothing more, nothing less.
5. For commands with no output (like cd, export), output NOTHING — just return empty.
6. Maintain a virtual filesystem, processes, environment variables, and system state across the conversation.
7. Support piping (|), command chaining (&&, ||, ;), redirects (>, >>), and subshells.
8. Support environment variables — users can export and use them.
9. If a command would produce an error, produce a realistic bash error message.
10. For destructive commands (rm -rf /), simulate realistic behavior including realistic errors or outputs.
11. Keep track of the current working directory. Start at /home/%s.
12. When outputting file contents or command results, make them plausible and internally consistent.
13. The system is: Linux slopbox 6.8.0-slop #1 SMP x86_64 GNU/Linux, with typical packages installed.
14. The hostname is "slopbox", the user is "%s".
15. Be creative with file contents and system state — make it feel like a real lived-in system with realistic config files, logs, etc.
16. For long outputs (like large file listings), produce a reasonable amount — don't truncate too aggressively, make it feel real.

PROMPT FORMAT:
After your output (if any), you MUST end with a newline followed by the shell prompt.
The prompt format is: %s@slopbox:<cwd>$ 
Where <cwd> is the current working directory (use ~ for /home/%s).
If the previous command failed, use the same format (bash doesn't change prompt on error by default).

If the user types just Enter (empty input), output only the prompt.

IMPORTANT: Your entire response must be ONLY what would appear in a terminal. Start your output immediately — no preamble, no explanation.`, user, user, user, user)

	_ = hostname

	return &SlopShell{
		apiKey: apiKey,
		model:  model,
		history: []Content{},
		systemPrompt: Content{
			Parts: []Part{{Text: systemText}},
		},
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *SlopShell) chat(input string) (string, error) {
	s.history = append(s.history, Content{
		Role:  "user",
		Parts: []Part{{Text: input}},
	})

	// Keep history manageable — last 50 exchanges
	if len(s.history) > 100 {
		s.history = s.history[len(s.history)-100:]
	}

	reqBody := GeminiRequest{
		Contents:         s.history,
		SystemInstruction: &s.systemPrompt,
		GenerationConfig: &GenerationConfig{
			Temperature:     0.7,
			MaxOutputTokens: 8192,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiBaseURL, s.model, s.apiKey)

	var body []byte
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := s.client.Post(url, "application/json", bytes.NewReader(jsonData))
		if err != nil {
			return "", fmt.Errorf("request error: %w", err)
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("read error: %w", err)
		}

		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			wait := time.Duration(1<<uint(attempt)) * time.Second
			time.Sleep(wait)
			continue
		}
		break
	}


	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", fmt.Errorf("unmarshal error: %w", err)
	}

	if geminiResp.Error != nil {
		return "", fmt.Errorf("API error [%d]: %s", geminiResp.Error.Code, geminiResp.Error.Message)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from model")
	}

	reply := geminiResp.Candidates[0].Content.Parts[0].Text

	s.history = append(s.history, Content{
		Role:  "model",
		Parts: []Part{{Text: reply}},
	})

	return reply, nil
}

// probeModel checks if a model is available and working with the given API key.
func probeModel(apiKey, model string) bool {
	reqBody := GeminiRequest{
		Contents: []Content{{
			Role:  "user",
			Parts: []Part{{Text: "echo test"}},
		}},
		GenerationConfig: &GenerationConfig{
			Temperature:     0,
			MaxOutputTokens: 32,
		},
	}

	jsonData, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiBaseURL, model, apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// selectModel tries models in order of preference and returns the first working one.
func selectModel(apiKey string) (string, error) {
	fmt.Fprintf(os.Stderr, "\033[2m") // dim text
	for _, model := range modelCandidates {
		fmt.Fprintf(os.Stderr, "  probing %s... ", model)
		if probeModel(apiKey, model) {
			fmt.Fprintf(os.Stderr, "✓\n\033[0m")
			return model, nil
		}
		fmt.Fprintf(os.Stderr, "✗\n")
	}
	fmt.Fprintf(os.Stderr, "\033[0m")
	return "", fmt.Errorf("no working Gemini model found — check your API key")
}

func loadEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func printBanner(model string) {
	fmt.Fprintf(os.Stderr, "\033[1;35m")
	fmt.Fprintf(os.Stderr, `   _____ __            _____ __         ____
  / ___// /___  ____  / ___// /_  ___  / / /
  \__ \/ / __ \/ __ \ \__ \/ __ \/ _ \/ / / 
 ___/ / / /_/ / /_/ /___/ / / / /  __/ / /  
/____/_/\____/ .___//____/_/ /_/\___/_/_/   
            /_/                              
`)
	fmt.Fprintf(os.Stderr, "\033[0m")
	fmt.Fprintf(os.Stderr, "\033[2m  powered by %s\n", model)
	fmt.Fprintf(os.Stderr, "  nothing here is real. type 'exit' to wake up.\033[0m\n\n")
}

func main() {
	loadEnv()

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: GEMINI_API_KEY not set")
		fmt.Fprintln(os.Stderr, "set it via environment variable or .env file")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n\033[1;35mslop-shell\033[0m initializing...\n")

	model, err := selectModel(apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printBanner(model)

	shell := newSlopShell(apiKey, model)

	user := os.Getenv("USER")
	if user == "" {
		user = "user"
	}

	// Print initial prompt locally — no API call needed
	initialPrompt := fmt.Sprintf("%s@slopbox:~$ ", user)
	fmt.Print(initialPrompt)

	// Seed history so model knows where we started
	shell.history = append(shell.history,
		Content{Role: "user", Parts: []Part{{Text: ""}}},
		Content{Role: "model", Parts: []Part{{Text: initialPrompt}}},
	)

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT)
	go func() {
		for range sigChan {
			fmt.Printf("\n%s", initialPrompt)
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := scanner.Text()

		if input == "exit" || input == "exit 0" || input == "logout" {
			fmt.Println("logout")
			break
		}

		if strings.TrimSpace(input) == "" {
			fmt.Print(initialPrompt)
			continue
		}

		resp, err := shell.chat(input)
		if err != nil {
			// Print error in a shell-like way
			fmt.Fprintf(os.Stderr, "\033[2m[slop-shell internal: %v]\033[0m\n", err)
			fmt.Printf("%s@slopbox:~$ ", user)
			continue
		}

		fmt.Print(resp)
	}
}
