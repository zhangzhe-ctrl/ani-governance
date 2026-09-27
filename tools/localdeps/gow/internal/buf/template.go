package buf

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// genTemplate is the part of a buf template that the locked API chain must know before it writes
// anything: the plugins a template invokes, the inputs it reads, the directories it clears and
// where it writes. It is deliberately a narrow reader for this repository's v2 templates rather
// than a general YAML parser, because the tool is not allowed to grow a new dependency, and
// because anything it cannot read has to fail loudly instead of being skipped.
type genTemplate struct {
	Name       string
	Path       string
	Clean      bool
	Plugins    []pluginRef
	InputDirs  []string
	InputPaths []string
	Outs       []string
}

type pluginRef struct {
	// Kind is "path" for a bare executable name, "command" for a quoted program plus
	// arguments, or "remote" for a buf.build plugin.
	Kind string
	// Program is the PATH name, the first word of the command, or the remote module.
	Program string
	Args    []string
	Raw     string
}

// goRunModule returns the module@version of a `go run` plugin invocation, if that is what it is.
func (p pluginRef) goRunModule() (string, bool) {
	if p.Kind != "command" || p.Program != "go" || len(p.Args) < 2 || p.Args[0] != "run" {
		return "", false
	}
	for _, a := range p.Args[1:] {
		if strings.Contains(a, "@") {
			return a, true
		}
	}
	return "", false
}

// parseGenTemplate reads one template file and reports what it will run and write.
func parseGenTemplate(path string) (*genTemplate, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	t := &genTemplate{Name: filepath.Base(path), Path: path}
	inInputPaths := false
	pathsIndent := -1
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		raw := sc.Text()
		line := strings.TrimSpace(stripComment(raw))
		if line == "" {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		// Input path entries are the list items directly under a `paths:` key. Any other key at
		// or above that indentation ends the list, which is what keeps `opt:` entries such as
		// `- paths=source_relative` from being mistaken for generation inputs.
		if inInputPaths && (indent <= pathsIndent || !strings.HasPrefix(line, "- ")) {
			inInputPaths = false
			pathsIndent = -1
		}
		switch {
		case inInputPaths:
			t.InputPaths = append(t.InputPaths, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
			continue
		case indent == 0 && strings.HasPrefix(line, "clean:"):
			v, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(line, "clean:")))
			if err != nil {
				return nil, fmt.Errorf("%s:%d: unreadable clean: %q", path, n, line)
			}
			t.Clean = v
		case line == "paths:":
			inInputPaths, pathsIndent = true, indent
		case strings.HasPrefix(line, "- directory:"):
			t.InputDirs = append(t.InputDirs, strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
		case strings.HasPrefix(line, "local:") || strings.HasPrefix(line, "- local:"):
			ref, err := parsePluginRef(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "- "), "local:")))
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, n, err)
			}
			t.Plugins = append(t.Plugins, ref)
		case strings.HasPrefix(line, "out:"):
			t.Outs = append(t.Outs, strings.TrimSpace(strings.TrimPrefix(line, "out:")))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(t.Plugins) == 0 {
		return nil, fmt.Errorf("%s declares no local plugins", path)
	}
	if len(t.Outs) == 0 {
		return nil, fmt.Errorf("%s declares no out: directory", path)
	}
	return t, nil
}

func parsePluginRef(value string) (pluginRef, error) {
	if value == "" {
		return pluginRef{}, fmt.Errorf("empty local: plugin")
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		parts := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
		for i, p := range parts {
			parts[i] = strings.Trim(p, `"', `)
		}
		if len(parts) == 0 {
			return pluginRef{}, fmt.Errorf("empty local: command list")
		}
		return pluginRef{Kind: "command", Program: parts[0], Args: parts[1:], Raw: value}, nil
	}
	if strings.HasPrefix(value, "buf.build/") {
		return pluginRef{Kind: "remote", Program: value, Raw: value}, nil
	}
	if strings.Contains(value, "/") {
		// A relative or absolute executable path, e.g. ../tools/bin/protoc-gen-go-redact.
		return pluginRef{Kind: "file", Program: value, Raw: value}, nil
	}
	if strings.ContainsAny(value, " \t") || strings.HasPrefix(value, `"`) {
		return pluginRef{}, fmt.Errorf("unsupported local: form %q", value)
	}
	return pluginRef{Kind: "path", Program: value, Raw: value}, nil
}

// stripComment removes a trailing or leading YAML comment while leaving quoted values alone.
func stripComment(line string) string {
	inSq, inDq := false, false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !inDq {
				inSq = !inSq
			}
		case '"':
			if !inSq {
				inDq = !inDq
			}
		case '#':
			if !inSq && !inDq {
				if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
					return line[:i]
				}
			}
		}
	}
	return line
}
