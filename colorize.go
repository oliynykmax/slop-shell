package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ANSI color codes (modern cohesive palette)
const (
	colorReset     = "\033[0m"
	colorBold      = "\033[1m"
	colorDim       = "\033[2m"
	colorUnderline = "\033[4m"

	// Core colors (slightly desaturated for cohesion)
	colorRed     = "\033[38;5;203m"
	colorGreen   = "\033[38;5;114m"
	colorYellow  = "\033[38;5;215m"
	colorBlue    = "\033[38;5;111m"
	colorMagenta = "\033[38;5;176m"
	colorCyan    = "\033[38;5;87m"
	colorWhite   = "\033[38;5;252m"
	colorOrange  = "\033[38;5;208m"
	colorPurple  = "\033[38;5;141m"

	// Bright variants for emphasis
	colorBrightRed    = "\033[1;38;5;203m"
	colorBrightGreen  = "\033[1;38;5;114m"
	colorBrightBlue   = "\033[1;38;5;111m"
	colorBrightYellow = "\033[1;38;5;215m"
	colorBrightCyan   = "\033[1;38;5;87m"
)

// File extension → color mappings
var archiveExts = map[string]bool{
	".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".zip": true,
	".rar": true, ".7z": true, ".tgz": true, ".deb": true, ".rpm": true,
	".zst": true, ".lz4": true, ".lzma": true, ".cab": true, ".msi": true,
	".apk": true, ".ipa": true, ".jar": true, ".war": true, ".ear": true,
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
	".svg": true, ".webp": true, ".ico": true, ".tiff": true, ".tif": true,
	".heic": true, ".heif": true, ".avif": true, ".raw": true, ".cr2": true,
	".nef": true, ".arw": true, ".dng": true, ".psd": true, ".xcf": true,
}

var execExts = map[string]bool{
	".sh": true, ".bash": true, ".py": true, ".rb": true, ".pl": true,
	".js": true, ".ts": true, ".mjs": true, ".cjs": true,
	".php": true, ".lua": true, ".r": true, ".jl": true,
	".fish": true, ".zsh": true, ".ps1": true, ".bat": true, ".cmd": true,
}

var codeExts = map[string]bool{
	".go": true, ".rs": true, ".c": true, ".h": true, ".cpp": true,
	".hpp": true, ".cc": true, ".cxx": true, ".java": true, ".kt": true,
	".scala": true, ".cs": true, ".fs": true, ".vb": true,
	".swift": true, ".m": true, ".mm": true,
	".zig": true, ".nim": true, ".cr": true, ".ex": true, ".exs": true,
	".ml": true, ".mli": true, ".hs": true, ".lhs": true,
	".clj": true, ".cljs": true, ".cljc": true, ".edn": true,
	".elm": true, ".purs": true, ".dhall": true,
}

var configExts = map[string]bool{
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".ini": true, ".cfg": true, ".conf": true, ".config": true,
	".xml": true, ".plist": true, ".properties": true,
	".env": true, ".envrc": true, ".flaskenv": true,
}

var docExts = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".rst": true,
	".adoc": true, ".asciidoc": true, ".tex": true, ".ltx": true,
	".pdf": true, ".epub": true, ".mobi": true,
	".doc": true, ".docx": true, ".odt": true, ".rtf": true,
}

var dataExts = map[string]bool{
	".csv": true, ".tsv": true, ".jsonl": true, ".ndjson": true,
	".sqlite": true, ".db": true, ".sql": true, ".duckdb": true,
	".parquet": true, ".avro": true, ".orc": true, ".feather": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".webm": true, ".mov": true,
	".avi": true, ".flv": true, ".wmv": true, ".m4v": true,
	".mpg": true, ".mpeg": true, ".3gp": true, ".ogv": true,
}

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".wav": true, ".ogg": true,
	".m4a": true, ".aac": true, ".opus": true, ".wma": true,
	".aiff": true, ".aif": true, ".ape": true,
}

var fontExts = map[string]bool{
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true,
	".eot": true, ".fon": true, ".bdf": true, ".pcf": true,
}

// Common directory names (for simple ls output without -l)
var commonDirs = map[string]bool{
	"Desktop": true, "Documents": true, "Downloads": true, "Music": true,
	"Pictures": true, "Videos": true, "Public": true, "Templates": true,
	"bin": true, "sbin": true, "lib": true, "lib64": true, "etc": true,
	"var": true, "tmp": true, "usr": true, "opt": true, "srv": true,
	"home": true, "root": true, "boot": true, "dev": true, "proc": true,
	"sys": true, "mnt": true, "media": true, "run": true, "snap": true,
	"projects": true, "scripts": true, "src": true, "pkg": true,
	"node_modules": true, "vendor": true, "target": true, ".git": true,
	".config": true, ".local": true, ".cache": true, ".ssh": true,
}

// Shell keywords and common commands used by command-line syntax highlighter.
var shellKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"for": true, "while": true, "do": true, "done": true,
	"case": true, "esac": true, "in": true, "function": true,
	"time": true, "coproc": true, "select": true,
}

var commonCommands = map[string]bool{
	"ls": true, "cd": true, "pwd": true, "cat": true, "less": true, "more": true,
	"head": true, "tail": true, "grep": true, "rg": true, "find": true, "locate": true,
	"sed": true, "awk": true, "cut": true, "sort": true, "uniq": true, "wc": true,
	"xargs": true, "tee": true, "tr": true,
	"cp": true, "mv": true, "rm": true, "mkdir": true, "rmdir": true, "touch": true,
	"chmod": true, "chown": true, "ln": true, "stat": true,
	"git": true, "docker": true, "kubectl": true, "ssh": true, "scp": true,
	"curl": true, "wget": true, "ping": true,
	"apt": true, "apt-get": true, "dpkg": true, "npm": true, "pnpm": true, "yarn": true,
	"pip": true, "pip3": true, "python": true, "python3": true,
	"go": true, "cargo": true, "rustc": true, "node": true,
	"make": true, "cmake": true, "ninja": true,
	"export": true, "unset": true, "alias": true, "source": true, "env": true,
	"sudo": true, "su": true,
}

// Regex patterns
var (
	// ls -l line: permissions, links, owner, group, size, date, name
	lsLongRe = regexp.MustCompile(`^([d\-lbcps][rwx\-sST]{9})\s+(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\w+\s+\d+\s+[\d:]+)\s+(.+)$`)
	// total N line
	lsTotalRe = regexp.MustCompile(`^total \d+`)
	// Error patterns
	errorRe = regexp.MustCompile(`(?i)(error|fatal|failed|not found|no such file|permission denied|cannot |command not found|segmentation fault)`)
	// Warning patterns
	warningRe = regexp.MustCompile(`(?i)(warning|deprecated|caution)`)
	// Prompt line (don't colorize)
	promptRe = regexp.MustCompile(`\S+@slopbox:[^$#]*[#$]\s*$`)
	// Prompt with typed input (also don't colorize)
	promptWithInputRe = regexp.MustCompile(`^\S+@slopbox:[^$#]*[#$] .+`)
)

// colorizeOutput applies ANSI colors to shell output.
func colorizeOutput(output string) string {
	if output == "" {
		return output
	}

	lines := strings.Split(output, "\n")
	var result []string

	inLsBlock := false

	for i, line := range lines {
		// Don't colorize prompt lines
		if promptRe.MatchString(line) {
			result = append(result, line)
			continue
		}
		if promptWithInputRe.MatchString(line) {
			result = append(result, line)
			continue
		}

		// Don't colorize empty lines
		if strings.TrimSpace(line) == "" {
			result = append(result, line)
			inLsBlock = false
			continue
		}

		// Detect ls -l output
		if lsTotalRe.MatchString(line) {
			result = append(result, colorDim+line+colorReset)
			inLsBlock = true
			continue
		}

		if m := lsLongRe.FindStringSubmatch(line); m != nil {
			result = append(result, colorizeLsLong(m))
			inLsBlock = true
			continue
		}

		// Error lines
		if errorRe.MatchString(line) {
			result = append(result, colorRed+line+colorReset)
			continue
		}

		// Warning lines
		if warningRe.MatchString(line) {
			result = append(result, colorYellow+line+colorReset)
			continue
		}

		// Shell command line syntax highlighting (fish-like token classes).
		if colored, ok := colorizeShellSyntax(line); ok {
			result = append(result, colored)
			continue
		}

		// Simple ls output (space/tab separated words, no long format)
		// Heuristic: if line has multiple words, all look like filenames, and we're not in some other output
		if looksLikeSimpleLs(lines, i) {
			result = append(result, colorizeSimpleLs(line))
			continue
		}

		_ = inLsBlock
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// colorizeLsLong colorizes a single ls -l line given regex submatches.
func colorizeLsLong(m []string) string {
	perms := m[1]
	links := m[2]
	owner := m[3]
	group := m[4]
	size := m[5]
	date := m[6]
	name := m[7]

	// Colorize permissions
	coloredPerms := colorizePerms(perms)

	// Colorize the filename based on type
	coloredName := colorizeFilename(name, perms[0])

	return fmt.Sprintf("%s %s %s %s %s %s %s",
		coloredPerms, links, owner, group, size,
		colorDim+date+colorReset, coloredName)
}

func colorizePerms(perms string) string {
	var b strings.Builder
	for i, ch := range perms {
		switch {
		case i == 0 && ch == 'd':
			b.WriteString(colorBlue + string(ch) + colorReset)
		case i == 0 && ch == 'l':
			b.WriteString(colorCyan + string(ch) + colorReset)
		case ch == 'r':
			b.WriteString(colorYellow + string(ch) + colorReset)
		case ch == 'w':
			b.WriteString(colorRed + string(ch) + colorReset)
		case ch == 'x' || ch == 's' || ch == 'S' || ch == 't' || ch == 'T':
			b.WriteString(colorGreen + string(ch) + colorReset)
		default:
			b.WriteString(colorDim + string(ch) + colorReset)
		}
	}
	return b.String()
}

func colorizeFilename(name string, typeChar byte) string {
	// Handle symlinks: name -> target
	if idx := strings.Index(name, " -> "); idx >= 0 {
		linkName := name[:idx]
		target := name[idx+4:]
		return colorCyan + linkName + colorReset + " -> " + target
	}

	switch typeChar {
	case 'd':
		return colorBlue + name + colorReset
	case 'l':
		return colorCyan + name + colorReset
	default:
		return colorizeFileByExt(name)
	}
}

func colorizeFileByExt(name string) string {
	// Check for compound extensions like .tar.gz
	lower := strings.ToLower(name)
	ext := filepath.Ext(lower)

	if strings.Contains(lower, ".tar.") || archiveExts[ext] {
		return colorRed + name + colorReset
	}
	if imageExts[ext] {
		return colorMagenta + name + colorReset
	}
	if videoExts[ext] {
		return colorPurple + name + colorReset
	}
	if audioExts[ext] {
		return colorOrange + name + colorReset
	}
	if fontExts[ext] {
		return colorCyan + name + colorReset
	}
	if codeExts[ext] {
		return colorBrightBlue + name + colorReset
	}
	if configExts[ext] {
		return colorYellow + name + colorReset
	}
	if docExts[ext] {
		return colorBrightCyan + name + colorReset
	}
	if dataExts[ext] {
		return colorBrightGreen + name + colorReset
	}
	if execExts[ext] {
		return colorBrightGreen + name + colorReset
	}
	// Dotfiles
	if strings.HasPrefix(name, ".") {
		return colorDim + name + colorReset
	}
	return name
}

// looksLikeSimpleLs detects if a line is likely simple ls output.
// Heuristic: multiple whitespace-separated tokens that look like filenames,
// following or near other similar lines.
func looksLikeSimpleLs(lines []string, idx int) bool {
	line := strings.TrimSpace(lines[idx])
	if line == "" {
		return false
	}
	if looksLikeCommandLine(line) {
		return false
	}

	words := strings.Fields(line)
	if len(words) < 2 {
		return false
	}

	// Check if most words look like filenames
	filenameCount := 0
	for _, w := range words {
		if looksLikeFilename(w) {
			filenameCount++
		}
	}

	// At least 70% should look like filenames
	return float64(filenameCount)/float64(len(words)) >= 0.7
}

func looksLikeFilename(s string) bool {
	// Filenames: start with letter/dot, contain only reasonable chars
	if s == "" {
		return false
	}
	// Must not contain = or other non-filename chars
	for _, ch := range s {
		if ch == '=' || ch == '(' || ch == ')' || ch == '{' || ch == '}' || ch == ';' || ch == ':' || ch == ',' {
			return false
		}
	}
	// Has extension or is a known dir name or starts with dot
	if filepath.Ext(s) != "" || commonDirs[s] || strings.HasPrefix(s, ".") {
		return true
	}
	// Capitalized single word (like Desktop, Documents)
	if len(s) > 1 && s[0] >= 'A' && s[0] <= 'Z' {
		return true
	}
	// Otherwise require a single token (no spaces) that contains at least
	// one ASCII letter — otherwise pure digits/symbols like "1" or "42"
	// would match and the simple-ls heuristic would miscolor command
	// output like `echo 1 1`.
	if strings.Contains(s, " ") {
		return false
	}
	hasLetter := false
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			hasLetter = true
			break
		}
	}
	return hasLetter
}

func colorizeSimpleLs(line string) string {
	words := strings.Fields(line)
	var colored []string

	for _, word := range words {
		colored = append(colored, colorizeSimpleEntry(word))
	}

	// Preserve original spacing pattern
	result := line
	for i, word := range words {
		if i < len(colored) {
			result = strings.Replace(result, word, colored[i], 1)
		}
	}
	return result
}

func colorizeSimpleEntry(name string) string {
	lower := strings.ToLower(name)
	ext := filepath.Ext(lower)

	// Known directory names
	if commonDirs[name] {
		return colorBlue + name + colorReset
	}

	// Archive extensions
	if strings.Contains(lower, ".tar.") || archiveExts[ext] {
		return colorRed + name + colorReset
	}

	// Image extensions
	if imageExts[ext] {
		return colorMagenta + name + colorReset
	}
	if videoExts[ext] {
		return colorPurple + name + colorReset
	}
	if audioExts[ext] {
		return colorOrange + name + colorReset
	}
	if fontExts[ext] {
		return colorCyan + name + colorReset
	}
	if codeExts[ext] {
		return colorBrightBlue + name + colorReset
	}
	if configExts[ext] {
		return colorYellow + name + colorReset
	}
	if docExts[ext] {
		return colorBrightCyan + name + colorReset
	}
	if dataExts[ext] {
		return colorBrightGreen + name + colorReset
	}

	// Executable extensions
	if execExts[ext] {
		return colorBrightGreen + name + colorReset
	}

	// Dotfiles/hidden
	if strings.HasPrefix(name, ".") {
		return colorDim + name + colorReset
	}

	// No extension = likely a directory
	if filepath.Ext(name) == "" && len(name) > 0 {
		return colorBlue + name + colorReset
	}

	return name
}

func colorizeShellSyntax(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !looksLikeCommandLine(trimmed) {
		return "", false
	}

	var out strings.Builder
	expectCommand := true

	for i := 0; i < len(line); {
		ch := line[i]

		if ch == ' ' || ch == '\t' {
			out.WriteByte(ch)
			i++
			continue
		}

		if ch == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			out.WriteString(colorDim)
			out.WriteString(line[i:])
			out.WriteString(colorReset)
			break
		}

		if tok, n, isCtrl := consumeOperator(line, i); n > 0 {
			out.WriteString(colorPurple)
			out.WriteString(tok)
			out.WriteString(colorReset)
			i += n
			if isCtrl {
				expectCommand = true
			}
			continue
		}

		if ch == '\'' || ch == '"' {
			quote := ch
			j := i + 1
			for j < len(line) {
				if line[j] == '\\' && j+1 < len(line) {
					j += 2
					continue
				}
				if line[j] == quote {
					j++
					break
				}
				j++
			}
			out.WriteString(colorGreen)
			out.WriteString(line[i:j])
			out.WriteString(colorReset)
			i = j
			expectCommand = false
			continue
		}

		if ch == '$' {
			j := i + 1
			if j < len(line) && line[j] == '{' {
				j++
				for j < len(line) && line[j] != '}' {
					j++
				}
				if j < len(line) {
					j++
				}
			} else if j < len(line) && line[j] == '(' {
				depth := 1
				j++
				for j < len(line) && depth > 0 {
					if line[j] == '(' {
						depth++
					} else if line[j] == ')' {
						depth--
					}
					j++
				}
			} else {
				for j < len(line) && isVarChar(line[j]) {
					j++
				}
			}
			out.WriteString(colorCyan)
			out.WriteString(line[i:j])
			out.WriteString(colorReset)
			i = j
			expectCommand = false
			continue
		}

		j := i
		for j < len(line) {
			if line[j] == ' ' || line[j] == '\t' || line[j] == '\'' || line[j] == '"' || line[j] == '$' {
				break
			}
			if _, n, _ := consumeOperator(line, j); n > 0 {
				break
			}
			j++
		}

		word := line[i:j]
		switch {
		case strings.HasPrefix(word, "-"):
			out.WriteString(colorYellow + word + colorReset)
			expectCommand = false
		case isEnvAssignment(word):
			out.WriteString(colorCyan + word + colorReset)
		case shellKeywords[word]:
			out.WriteString(colorPurple + word + colorReset)
			expectCommand = true
		case expectCommand:
			out.WriteString(colorBrightBlue + word + colorReset)
			expectCommand = false
		case looksLikePath(word):
			out.WriteString(colorBlue + word + colorReset)
		default:
			out.WriteString(word)
		}
		i = j
	}

	return out.String(), true
}

func looksLikeCommandLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	if strings.HasPrefix(line, "#") {
		return true
	}

	if strings.ContainsAny(line, "|&;<>()$`\"'") {
		return true
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false
	}

	idx := 0
	for idx < len(parts) && isEnvAssignment(parts[idx]) {
		idx++
	}
	if idx >= len(parts) {
		return true
	}

	cmd := parts[idx]
	if cmd == "sudo" && idx+1 < len(parts) {
		cmd = parts[idx+1]
	}

	if shellKeywords[cmd] || commonCommands[cmd] {
		return true
	}
	if strings.HasPrefix(cmd, "./") || strings.HasPrefix(cmd, "/") || strings.HasPrefix(cmd, "~/") {
		return true
	}
	if len(parts) > idx+1 && strings.HasPrefix(parts[idx+1], "-") {
		return true
	}

	return false
}

func consumeOperator(line string, i int) (string, int, bool) {
	if i >= len(line) {
		return "", 0, false
	}
	if strings.HasPrefix(line[i:], "&&") || strings.HasPrefix(line[i:], "||") {
		return line[i : i+2], 2, true
	}
	if strings.HasPrefix(line[i:], ">>") || strings.HasPrefix(line[i:], "<<") {
		return line[i : i+2], 2, false
	}
	if i+2 <= len(line) && line[i] >= '0' && line[i] <= '9' && line[i+1] == '>' {
		if i+3 <= len(line) && line[i+2] == '>' {
			return line[i : i+3], 3, false
		}
		return line[i : i+2], 2, false
	}
	if strings.HasPrefix(line[i:], "&>") {
		return line[i : i+2], 2, false
	}
	if strings.HasPrefix(line[i:], "|") || strings.HasPrefix(line[i:], ";") {
		return line[i : i+1], 1, true
	}
	if strings.HasPrefix(line[i:], "<") || strings.HasPrefix(line[i:], ">") {
		return line[i : i+1], 1, false
	}
	return "", 0, false
}

func isVarChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isEnvAssignment(word string) bool {
	eq := strings.IndexByte(word, '=')
	if eq <= 0 {
		return false
	}
	name := word[:eq]
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if i == 0 {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_') {
				return false
			}
			continue
		}
		if !isVarChar(ch) {
			return false
		}
	}
	return true
}

func looksLikePath(word string) bool {
	return strings.HasPrefix(word, "./") || strings.HasPrefix(word, "../") || strings.HasPrefix(word, "~/") || strings.HasPrefix(word, "/") || strings.Contains(word, "/")
}
