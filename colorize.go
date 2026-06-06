package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
	colorRed     = "\033[1;31m"
	colorGreen   = "\033[1;32m"
	colorYellow  = "\033[0;33m"
	colorBlue    = "\033[1;34m"
	colorMagenta = "\033[1;35m"
	colorCyan    = "\033[0;36m"
	colorWhite   = "\033[1;37m"
)

// File extension → color mappings
var archiveExts = map[string]bool{
	".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".zip": true,
	".rar": true, ".7z": true, ".tgz": true, ".deb": true, ".rpm": true,
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
	".svg": true, ".webp": true, ".ico": true,
}

var execExts = map[string]bool{
	".sh": true, ".bash": true, ".py": true, ".rb": true, ".pl": true,
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
	if strings.Contains(lower, ".tar.") || archiveExts[filepath.Ext(lower)] {
		return colorRed + name + colorReset
	}
	if imageExts[filepath.Ext(lower)] {
		return colorMagenta + name + colorReset
	}
	if execExts[filepath.Ext(lower)] {
		return colorGreen + name + colorReset
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

	// Known directory names
	if commonDirs[name] {
		return colorBlue + name + colorReset
	}

	// Archive extensions
	if strings.Contains(lower, ".tar.") || archiveExts[filepath.Ext(lower)] {
		return colorRed + name + colorReset
	}

	// Image extensions
	if imageExts[filepath.Ext(lower)] {
		return colorMagenta + name + colorReset
	}

	// Executable extensions
	if execExts[filepath.Ext(lower)] {
		return colorGreen + name + colorReset
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
