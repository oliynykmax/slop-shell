package main

import (
	"bufio"
	"bytes"
	"encoding/json"

	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chzyer/readline"
)

const (
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"
)

// Models to try in order of preference (newest/best first).
var modelCandidates = []string{
	"gemini-3.5-flash",
	"gemini-3.1-pro",
	"gemini-2.5-flash",
	"gemini-2.5-pro",
	"gemini-2.0-flash",
	"gemini-1.5-pro",
	"gemini-1.5-flash",
}

// --- Gemini API types ---

type GeminiRequest struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
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

// --- Streaming types ---

type StreamChunk struct {
	Candidates []StreamCandidate `json:"candidates"`
	Error      *APIError         `json:"error,omitempty"`
}

type StreamCandidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
}

// --- Shell state ---

type SlopShell struct {
	apiKey       string
	model        string
	history      []Content
	systemPrompt Content
	client       *http.Client
	user         string
	isRoot       bool
	streaming    bool
	colorize     bool
}

func buildSystemPrompt(user string) string {
	return fmt.Sprintf(`You are a fully hallucinated Unix/Linux shell. You must pretend to be a real bash shell running on a Linux system. You are NOT an AI assistant — you ARE a shell.

CRITICAL RULES:
1. NEVER use markdown formatting. No backticks, no bold, no headers, no bullet points. Output raw text exactly like a terminal would.
2. NEVER break character. You are a shell. If someone asks "are you AI?" respond like a shell would — command not found or similar.
3. NEVER say "I'm an AI" or "I can't actually execute commands" or anything like that. You ARE the shell.
4. Respond ONLY with what a real terminal would output. Nothing more, nothing less.
5. For commands with no output (like cd, export), output NOTHING — just return the prompt.
6. Maintain a virtual filesystem, processes, environment variables, and system state across the conversation.
7. Support piping (|), command chaining (&&, ||, ;), redirects (>, >>), and subshells.
8. Support environment variables — users can export and use them.
9. If a command would produce an error, produce a realistic bash error message.
10. For destructive commands (rm -rf /), simulate realistic behavior including realistic errors or outputs.
11. Keep track of the current working directory. Start at /home/%s.
12. When outputting file contents or command results, make them plausible and internally consistent.
13. The hostname is "slopbox", the user is "%s".
14. Be creative with file contents and system state — make it feel like a real lived-in system with realistic config files, logs, etc.
15. For long outputs (like large file listings), produce a reasonable amount — don't truncate too aggressively, make it feel real.

SYSTEM:
The system runs Ubuntu 24.04 LTS. Use apt as the package manager.
The kernel is: Linux slopbox 6.8.0-slop #1 SMP x86_64 GNU/Linux

OUTPUT FORMAT:
Do NOT use any ANSI escape codes or color codes in your output. Output plain text only. Colors are handled externally.

SUDO SUPPORT:
- When the user runs sudo commands, simulate them as if the user has sudo access.
- After sudo, the command runs as root. If it's "sudo su" or "sudo -i" or "sudo bash", switch to a root shell.
- When in root shell, the prompt changes to: root@slopbox:<cwd># 
- The user can type "exit" from root shell to go back to normal user.

PACKAGE MANAGER:
- apt, apt-get, dpkg, pip, pip3, npm, cargo, go install — all "work"
- Show realistic install progress with download bars, dependency resolution, etc.
- After installing a package, it should be "available" in subsequent commands
- pip install should show "Successfully installed package-x.y.z"
- apt install should show realistic output with [Y/n] already answered

PROMPT FORMAT:
After your output (if any), you MUST end with a newline followed by the shell prompt.
For normal user: %s@slopbox:<cwd>$ 
For root: root@slopbox:<cwd># 
Where <cwd> is the current working directory (use ~ for the user's home).

IMPORTANT: Your entire response must be ONLY what would appear in a terminal. Start your output immediately — no preamble, no explanation.`, user, user, user)
}

func newSlopShell(apiKey, model string, streaming bool) *SlopShell {
	user := os.Getenv("USER")
	if user == "" {
		user = "user"
	}

	systemText := buildSystemPrompt(user)

	return &SlopShell{
		apiKey:  apiKey,
		model:   model,
		history: []Content{},
		systemPrompt: Content{
			Parts: []Part{{Text: systemText}},
		},
		client:    &http.Client{Timeout: 120 * time.Second},
		user:      user,
		isRoot:    false,
		streaming: streaming,
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
		Contents:          s.history,
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

	if s.streaming {
		return s.chatStream(jsonData)
	}
	return s.chatSync(jsonData)
}

func (s *SlopShell) chatSync(jsonData []byte) (string, error) {
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

func (s *SlopShell) chatStream(jsonData []byte) (string, error) {
	url := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse&key=%s", geminiBaseURL, s.model, s.apiKey)

	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = s.client.Post(url, "application/json", bytes.NewReader(jsonData))
		if err != nil {
			return "", fmt.Errorf("request error: %w", err)
		}

		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			resp.Body.Close()
			wait := time.Duration(1<<uint(attempt)) * time.Second
			time.Sleep(wait)
			continue
		}
		break
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		var apiErr struct {
			Error *APIError `json:"error"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != nil {
			return "", fmt.Errorf("API error [%d]: %s", apiErr.Error.Code, apiErr.Error.Message)
		}
		return "", fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}

	// Stream and print line-by-line with optional colorization
	var fullText strings.Builder
	var lineBuf strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	printLine := func(line string) {
		if s.colorize {
			fmt.Print(colorizeOutput(line))
		} else {
			fmt.Print(line)
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Error != nil {
			return fullText.String(), fmt.Errorf("stream error [%d]: %s", chunk.Error.Code, chunk.Error.Message)
		}

		if len(chunk.Candidates) > 0 && len(chunk.Candidates[0].Content.Parts) > 0 {
			text := chunk.Candidates[0].Content.Parts[0].Text
			fullText.WriteString(text)

			// Buffer and print complete lines for colorization
			for _, ch := range text {
				if ch == '\n' {
					printLine(lineBuf.String() + "\n")
					lineBuf.Reset()
				} else {
					lineBuf.WriteRune(ch)
				}
			}
		}
	}

	// Flush remaining buffer (likely the prompt — don't print it, let readline handle it)
	// But if it's not a prompt, print it
	remaining := lineBuf.String()
	if remaining != "" && !promptRe.MatchString(remaining) {
		printLine(remaining)
	}

	reply := fullText.String()
	s.history = append(s.history, Content{
		Role:  "model",
		Parts: []Part{{Text: reply}},
	})

	return reply, nil
}

// chatForCompletion does a quick non-streaming call for tab completion.
func (s *SlopShell) chatForCompletion(partialInput string) []string {
	prompt := fmt.Sprintf(`The user has typed this partial command and pressed Tab:
%s

Return ONLY a newline-separated list of possible completions (the full completed words, not the whole command). 
If it looks like a path, complete the path. If it looks like a command, complete the command.
Return between 1-8 completions, most likely first. Just the completion words, nothing else.`, partialInput)

	reqBody := GeminiRequest{
		Contents: []Content{{
			Role:  "user",
			Parts: []Part{{Text: prompt}},
		}},
		SystemInstruction: &Content{
			Parts: []Part{{Text: "You are a bash shell tab-completion engine. Return only completion candidates, one per line. No explanations, no formatting, no markdown. Just the words."}},
		},
		GenerationConfig: &GenerationConfig{
			Temperature:     0.3,
			MaxOutputTokens: 256,
		},
	}

	// Include recent history for context
	if len(s.history) > 0 {
		recent := s.history
		if len(recent) > 10 {
			recent = recent[len(recent)-10:]
		}
		historyContext := "Recent shell history for context:\n"
		for _, h := range recent {
			if h.Role == "user" && len(h.Parts) > 0 {
				historyContext += "$ " + h.Parts[0].Text + "\n"
			}
		}
		reqBody.Contents = []Content{
			{Role: "user", Parts: []Part{{Text: historyContext + "\n" + prompt}}},
		}
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiBaseURL, s.model, s.apiKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil
	}

	text := geminiResp.Candidates[0].Content.Parts[0].Text
	lines := strings.Split(strings.TrimSpace(text), "\n")

	var completions []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			completions = append(completions, line)
		}
	}
	return completions
}

// --- MOTD ---

func generateMOTD(user string) string {
	now := time.Now()
	upDays := 12 + now.Day()%20
	upHours := now.Hour()
	upMins := now.Minute()

	load1 := 0.1 + float64(now.Second()%30)/100.0
	load5 := 0.05 + float64(now.Second()%20)/100.0
	load15 := 0.02 + float64(now.Second()%10)/100.0

	memTotal := 16384
	memUsed := 3200 + now.Second()*20
	memFree := memTotal - memUsed

	procs := 180 + now.Second()%40

	updates := 3 + now.Day()%15
	secUpdates := now.Day() % 4

	lastLogin := now.Add(-time.Duration(3+now.Hour()) * time.Hour).Format("Mon Jan 2 15:04:05 2006")
	lastIP := fmt.Sprintf("192.168.1.%d", 100+now.Second()%155)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\033[1;35mWelcome to slopbox!\033[0m\n\n"))
	b.WriteString(fmt.Sprintf("  \033[2mOS:\033[0m      Ubuntu 24.04 LTS (Noble Numbat)\n"))
	b.WriteString(fmt.Sprintf("  \033[2mKernel:\033[0m  Linux 6.8.0-slop x86_64\n"))
	b.WriteString(fmt.Sprintf("  \033[2mUptime:\033[0m  %d days, %d:%02d\n", upDays, upHours, upMins))
	b.WriteString(fmt.Sprintf("  \033[2mLoad:\033[0m    %.2f, %.2f, %.2f\n", load1, load5, load15))
	b.WriteString(fmt.Sprintf("  \033[2mMemory:\033[0m  %dMB / %dMB (\033[1;32m%dMB free\033[0m)\n", memUsed, memTotal, memFree))
	b.WriteString(fmt.Sprintf("  \033[2mProcs:\033[0m   %d\n\n", procs))
	b.WriteString(fmt.Sprintf("  \033[2mLast login:\033[0m %s from %s\n", lastLogin, lastIP))

	if updates > 0 {
		if secUpdates > 0 {
			b.WriteString(fmt.Sprintf("  \033[1;33m%d updates available (%d security)\033[0m\n", updates, secUpdates))
		} else {
			b.WriteString(fmt.Sprintf("  \033[2m%d updates available\033[0m\n", updates))
		}
	}

	b.WriteString("\n")
	return b.String()
}

// --- Model probing ---

func probeModel(apiKey, model string) (bool, error) {
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
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return true, nil
	}

	body, _ := io.ReadAll(resp.Body)
	var apiErr struct {
		Error *APIError `json:"error"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != nil {
		return false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiErr.Error.Message)
	}
	return false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
}

func selectModel(apiKey string) (string, error) {
	fmt.Fprintf(os.Stderr, "\033[2m")
	var lastErr error
	for _, model := range modelCandidates {
		fmt.Fprintf(os.Stderr, "  probing %s... ", model)
		ok, err := probeModel(apiKey, model)
		if ok {
			fmt.Fprintf(os.Stderr, "✓\n\033[0m")
			return model, nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, "✗\n")
	}
	fmt.Fprintf(os.Stderr, "\033[0m")
	
	if lastErr != nil {
		return "", fmt.Errorf("no working Gemini model found — check your API key (last error: %v)", lastErr)
	}
	return "", fmt.Errorf("no working Gemini model found — check your API key")
}

// --- Utilities ---

func loadEnv() {
	f, err := os.Open(".env")
	if err != nil {
		// Also try ~/.config/slop-shell/.env
		home, herr := os.UserHomeDir()
		if herr != nil {
			return
		}
		f, err = os.Open(filepath.Join(home, ".config", "slop-shell", ".env"))
		if err != nil {
			return
		}
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



// handleSudo processes sudo password prompt locally.
func handleSudo(rl *readline.Instance, input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "sudo ") {
		return input, false
	}

	// Prompt for password (accept anything)
	oldPrompt := rl.Config.Prompt
	rl.SetPrompt("[sudo] password for " + os.Getenv("USER") + ": ")

	// Read password with hidden input
	pw, err := rl.ReadPassword("[sudo] password for " + os.Getenv("USER") + ": ")
	if err != nil {
		rl.SetPrompt(oldPrompt)
		return "", true
	}
	_ = pw
	rl.SetPrompt(oldPrompt)

	return input, false
}

// --- Tab completion ---

type aiCompleter struct {
	shell *SlopShell
}

func (c *aiCompleter) Do(line []rune, pos int) ([][]rune, int) {
	input := string(line[:pos])
	if strings.TrimSpace(input) == "" {
		return nil, 0
	}

	// Find the last word for replacement length
	lastSpace := strings.LastIndex(input, " ")
	var lastWord string
	if lastSpace >= 0 {
		lastWord = input[lastSpace+1:]
	} else {
		lastWord = input
	}

	completions := c.shell.chatForCompletion(input)
	if len(completions) == 0 {
		return nil, 0
	}

	var results [][]rune
	for _, comp := range completions {
		// Only add the suffix that needs to be appended
		if strings.HasPrefix(comp, lastWord) {
			suffix := comp[len(lastWord):]
			results = append(results, []rune(suffix))
		} else {
			results = append(results, []rune(comp))
		}
	}

	return results, 0
}

// --- Main ---

func main() {
	loadEnv()

	noStream := false
	noMOTD := false
	noColor := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--no-stream":
			noStream = true
		case "--no-motd":
			noMOTD = true
		case "--no-color":
			noColor = true
		}
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: GEMINI_API_KEY not set")
		fmt.Fprintln(os.Stderr, "set it via environment variable or .env file")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\033[2m")

	model, err := selectModel(apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[0m")
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	streaming := !noStream
	shell := newSlopShell(apiKey, model, streaming)
	shell.colorize = !noColor

	user := shell.user

	if !noMOTD {
		fmt.Print(generateMOTD(user))
	}

	initialPrompt := fmt.Sprintf("%s@slopbox:~$ ", user)

	// Seed history so model knows where we started
	shell.history = append(shell.history,
		Content{Role: "user", Parts: []Part{{Text: ""}}},
		Content{Role: "model", Parts: []Part{{Text: initialPrompt}}},
	)

	// Set up readline with history file and AI tab completion
	historyFile := filepath.Join(os.TempDir(), "slop-shell-history")
	if home, err := os.UserHomeDir(); err == nil {
		historyFile = filepath.Join(home, ".slop_history")
	}

	completer := &aiCompleter{shell: shell}

	rl, err := readline.NewEx(&readline.Config{
		Prompt:            initialPrompt,
		HistoryFile:       historyFile,
		HistorySearchFold: true,
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		AutoComplete:      completer,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing readline: %v\n", err)
		os.Exit(1)
	}
	defer rl.Close()

	// Regex to extract the trailing prompt from model output
	promptRe := regexp.MustCompile(`(?m)(\S+@slopbox:[^$#]*[#$] )$`)

	for {
		line, err := rl.Readline()
		if err == readline.ErrInterrupt {
			// Ctrl+C — cancel current line, just like bash
			continue
		}
		if err != nil { // io.EOF (Ctrl+D)
			fmt.Println("logout")
			break
		}

		input := strings.TrimRight(line, " ")

		if input == "exit" || input == "exit 0" || input == "logout" {
			fmt.Println("logout")
			break
		}

		if strings.TrimSpace(input) == "" {
			continue
		}

		// Handle sudo password prompt locally
		input, skip := handleSudo(rl, input)
		if skip {
			continue
		}

		resp, err := shell.chat(input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[2m[slop-shell internal: %v]\033[0m\n", err)
			continue
		}

		// For streaming, output was already printed line-by-line
		// For non-streaming, print now with colorization
		if !streaming {
			var output string
			if loc := promptRe.FindStringIndex(resp); loc != nil {
				output = resp[:loc[0]]
				newPrompt := resp[loc[0]:loc[1]]
				rl.SetPrompt(newPrompt)
			} else {
				output = resp
			}

			if output != "" {
				if !noColor {
					fmt.Print(colorizeOutput(output))
				} else {
					fmt.Print(output)
				}
			}
		} else {
			// For streaming, just extract prompt for readline
			if loc := promptRe.FindStringIndex(resp); loc != nil {
				newPrompt := resp[loc[0]:loc[1]]
				rl.SetPrompt(newPrompt)
			}
		}

		// Track if we switched to/from root
		if strings.Contains(resp, "root@slopbox:") && strings.HasSuffix(strings.TrimSpace(resp), "#") {
			shell.isRoot = true
		} else if strings.Contains(resp, user+"@slopbox:") {
			shell.isRoot = false
		}
	}
}
