package main

import (
	"strings"
	"testing"
)

func TestColorizeLsLong_DirectFile(t *testing.T) {
	line := "-rw-r--r--  1 kort kort  807 Mar 20  2023 .profile"
	m := lsLongRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("ls -l line did not match: %q", line)
	}
	out := colorizeLsLong(m)

	// Filename should be present (possibly with ANSI wrappers).
	if !strings.Contains(out, ".profile") {
		t.Errorf("output missing filename: %q", out)
	}
	// Size column should be uncolored.
	if !strings.Contains(out, "807") {
		t.Errorf("output missing size: %q", out)
	}
	// Should include the ANSI reset at least once.
	if !strings.Contains(out, colorReset) {
		t.Errorf("output missing ANSI reset: %q", out)
	}
}

func TestColorizeLsLong_Directory(t *testing.T) {
	line := "drwxr-xr-x  8 kort kort 4096 Jun  6 14:32 ."
	m := lsLongRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("ls -l directory line did not match: %q", line)
	}
	out := colorizeLsLong(m)
	if !strings.Contains(out, colorBlue) {
		t.Errorf("expected directory name to be blue: %q", out)
	}
}

func TestColorizeLsLong_Symlink(t *testing.T) {
	line := "lrwxrwxrwx  1 root root   12 May 10 09:00 link -> target"
	m := lsLongRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("ls -l symlink line did not match: %q", line)
	}
	out := colorizeLsLong(m)
	if !strings.Contains(out, "->") {
		t.Errorf("output missing arrow: %q", out)
	}
	if !strings.Contains(out, colorCyan) {
		t.Errorf("expected symlink to be cyan: %q", out)
	}
}

func TestColorizeLsLong_ExecuteBit(t *testing.T) {
	line := "-rwxr-xr-x  1 kort kort  220 Mar 20  2023 .bashrc"
	m := lsLongRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("ls -l executable line did not match: %q", line)
	}
	out := colorizeLsLong(m)
	if !strings.Contains(out, colorGreen) {
		t.Errorf("expected executable bit to be green: %q", out)
	}
}

func TestLooksLikeFilename(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"file.txt", true},
		{".bashrc", true},
		{"Desktop", true},
		{"Documents", true},
		{"a", true},
		{"", false},
		{"foo bar", false},  // contains space
		{"foo=bar", false},  // contains =
		{"foo(bar)", false}, // contains parens
		{"foo;bar", false},  // contains ;
	}
	for _, tc := range cases {
		got := looksLikeFilename(tc.in)
		if got != tc.want {
			t.Errorf("looksLikeFilename(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLooksLikeSimpleLs(t *testing.T) {
	lines := []string{
		"total 48",
		"drwxr-xr-x  8 kort kort 4096 Jun  6 14:32 .",
		"drwxr-xr-x  3 root root 4096 Apr 20 09:58 ..",
		"-rw-------  1 kort kort  456 Jun  6 14:30 .bash_history",
		"",
		"Desktop  Documents  Downloads  Music  Pictures",
	}
	if !looksLikeSimpleLs(lines, 5) {
		t.Errorf("expected space-separated directory list to look like simple ls")
	}
	// Empty line — never look like ls.
	if looksLikeSimpleLs(lines, 4) {
		t.Errorf("empty line should not look like simple ls")
	}
	// Single-word line — not enough tokens.
	lines2 := []string{"hello"}
	if looksLikeSimpleLs(lines2, 0) {
		t.Errorf("single-word line should not look like simple ls")
	}
	// Prose with shell-punctuation should not look like ls.
	lines3 := []string{"hello, world; foo=bar"}
	if looksLikeSimpleLs(lines3, 0) {
		t.Errorf("prose with punctuation should not look like simple ls")
	}
}

func TestColorizeOutput_PreservesPrompts(t *testing.T) {
	in := "kort@slopbox:~$ ls -la\ntotal 48\nfile.txt\nkort@slopbox:~$ "
	out := colorizeOutput(in)
	// Prompt lines should be byte-identical (no color wrappers).
	for _, line := range strings.Split(in, "\n") {
		if !strings.Contains(out, line) {
			t.Errorf("output missing line %q in %q", line, out)
		}
	}
}

func TestColorizeOutput_ColorsErrors(t *testing.T) {
	in := "ls: cannot access 'foo': No such file or directory"
	out := colorizeOutput(in)
	if !strings.Contains(out, colorRed) {
		t.Errorf("expected error line to be red: %q", out)
	}
}

func TestColorizeOutput_ColorsWarnings(t *testing.T) {
	in := "WARNING: deprecated config syntax"
	out := colorizeOutput(in)
	if !strings.Contains(out, colorYellow) {
		t.Errorf("expected warning line to be yellow: %q", out)
	}
}

func TestColorizeFileByExt(t *testing.T) {
	cases := []struct {
		in        string
		wantColor string
	}{
		{"archive.tar.gz", colorRed},
		{"photo.png", colorMagenta},
		{"script.sh", colorGreen},
		{".hidden", colorDim},
		{"plain", ""}, // no extension, no color
	}
	for _, tc := range cases {
		out := colorizeFileByExt(tc.in)
		if tc.wantColor == "" {
			if strings.Contains(out, "\033[") {
				t.Errorf("colorizeFileByExt(%q) unexpectedly colored: %q", tc.in, out)
			}
		} else if !strings.Contains(out, tc.wantColor) {
			t.Errorf("colorizeFileByExt(%q) = %q, want color %q", tc.in, out, tc.wantColor)
		}
	}
}

func TestStreamSkipsPromptLines(t *testing.T) {
	// Simulate the runStream onDelta callback against chunks shaped like
	// the model's output, and confirm the trailing prompt line never
	// reaches printLine.
	cases := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{
			name:   "ls output + trailing prompt",
			chunks: []string{"Documents  Downloads  .bashrc\n", "kort@slopbox:~$ ", "\n"},
			want:   []string{"Documents  Downloads  .bashrc\n"},
		},
		{
			name:   "pwd output + prompt no trailing space",
			chunks: []string{"/home/kort\n", "kort@slopbox:~$", "\n"},
			want:   []string{"/home/kort\n"},
		},
		{
			name:   "root prompt",
			chunks: []string{"root:x:0:0:root:/root:/bin/bash\n", "root@slopbox:/etc# ", "\n"},
			want:   []string{"root:x:0:0:root:/root:/bin/bash\n"},
		},
		{
			name:   "no newline before prompt (buffered until end)",
			chunks: []string{"Documents  Downloads\n", "kort@slopbox:~$ "},
			want:   []string{"Documents  Downloads\n"},
		},
		{
			name:   "model emits NBSP after prompt (regex \\\\s doesn't match)",
			chunks: []string{"/home/kort\n", "kort@slopbox:~$\u00a0", "\n"},
			want:   []string{"/home/kort\n"},
		},
		{
			name:   "model emits tab after prompt",
			chunks: []string{"/home/kort\n", "kort@slopbox:~$\t", "\n"},
			want:   []string{"/home/kort\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var lineBuf strings.Builder
			var printed []string
			printLine := func(s string) { printed = append(printed, s) }

			for _, chunk := range tc.chunks {
				for _, ch := range chunk {
					if ch == '\n' {
						line := lineBuf.String()
						if !isPromptLine(line) {
							printLine(line + "\n")
						}
						lineBuf.Reset()
					} else {
						lineBuf.WriteRune(ch)
					}
				}
			}
			// End-of-stream flush (mirrors runStream)
			if rem := lineBuf.String(); rem != "" {
				flushLineBuffer(rem, printLine)
			}

			if len(printed) != len(tc.want) {
				t.Fatalf("got %d lines, want %d: %q", len(printed), len(tc.want), printed)
			}
			for i, w := range tc.want {
				if printed[i] != w {
					t.Errorf("line %d: got %q, want %q", i, printed[i], w)
				}
			}
		})
	}
}

func TestIsPromptLine(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"kort@slopbox:~$ ", true},
		{"kort@slopbox:~$", true},
		{"kort@slopbox:~# ", true},
		{"root@slopbox:/etc# ", true},
		{"kort@slopbox:~$\u00a0", true}, // NBSP
		{"kort@slopbox:~$\t", true},     // tab
		{"kort@slopbox:~$  ", true},     // extra spaces
		{"Documents  Downloads", false}, // not a prompt
		{"/home/kort", false},
		{"", false},
		{"welcome to slopbox", false},    // no @slopbox
		{"PS1='\\u@\\h:\\w\\$ '", false}, // prompt definition, ends with quote
	}
	for _, tc := range cases {
		got := isPromptLine(tc.in)
		if got != tc.want {
			t.Errorf("isPromptLine(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCompletionCacheLRU(t *testing.T) {
	c := newCompletionCache(2)
	c.put("a", []string{"x"})
	c.put("b", []string{"y"})
	c.put("c", []string{"z"}) // evicts "a"

	if _, ok := c.get("a"); ok {
		t.Error("expected 'a' to be evicted")
	}
	if v, ok := c.get("b"); !ok || len(v) != 1 || v[0] != "y" {
		t.Errorf("unexpected value for 'b': %v", v)
	}
	if v, ok := c.get("c"); !ok || len(v) != 1 || v[0] != "z" {
		t.Errorf("unexpected value for 'c': %v", v)
	}
}
