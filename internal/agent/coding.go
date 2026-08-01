package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CodingToolRegistry exposes a deliberately small set of tools for code agents.
// Unlike the legacy registry, every file operation is contained in one workspace
// and no shell command or outbound HTTP request is available to the model.
type CodingToolRegistry struct {
	workspace string
	tools     map[string]ToolFunction
}

func NewCodingToolRegistry(workspace string) (*CodingToolRegistry, error) {
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("invalid agent workspace %q", workspace)
	}
	r := &CodingToolRegistry{workspace: resolved, tools: make(map[string]ToolFunction)}
	r.registerTools()
	return r, nil
}

func (r *CodingToolRegistry) Workspace() string { return r.workspace }

func (r *CodingToolRegistry) Descriptions() string {
	return `- list_files: list repository files. Input: {"path":"."}.
- search: search text in workspace files. Input: {"query":"text","path":"."}.
- read_file: read one UTF-8 text file. Input: {"path":"relative/path"}.
- apply_patch: apply a unified diff inside the workspace. Input: {"patch":"diff --git a/file b/file..."}.
- run_check: run one safe verification command. Input: {"check":"go_test|go_vet|go_build|npm_test|npm_run_build|npm_run_lint"}.
- finish: only when the requested work and relevant verification are complete. Input: {"final_output":"summary"}.`
}

func (r *CodingToolRegistry) Execute(action string, input map[string]interface{}) (string, error) {
	fn, ok := r.tools[action]
	if !ok {
		return "", fmt.Errorf("tool %q is not available", action)
	}
	ctx, cancel := context.WithTimeout(context.Background(), codingToolTimeout())
	defer cancel()
	result, err := fn(ctx, input)
	if err != nil {
		return "", err
	}
	return truncateToolOutput(result), nil
}

func (r *CodingToolRegistry) registerTools() {
	r.tools["list_files"] = func(ctx context.Context, input map[string]interface{}) (string, error) {
		path, err := r.pathInput(input, "path", ".")
		if err != nil {
			return "", err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", err
		}
		var out []string
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			out = append(out, name)
		}
		return strings.Join(out, "\n"), nil
	}
	r.tools["read_file"] = func(ctx context.Context, input map[string]interface{}) (string, error) {
		path, err := r.pathInput(input, "path", "")
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%q is a directory", input["path"])
		}
		if info.Size() > codingReadLimit() {
			return "", fmt.Errorf("file exceeds %d byte read limit", codingReadLimit())
		}
		data, err := os.ReadFile(path)
		return string(data), err
	}
	r.tools["search"] = func(ctx context.Context, input map[string]interface{}) (string, error) {
		query, _ := input["query"].(string)
		if strings.TrimSpace(query) == "" {
			return "", fmt.Errorf("missing query")
		}
		path, err := r.pathInput(input, "path", ".")
		if err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, "rg", "-n", "--hidden", "--glob", "!.git", "--", query, path)
		cmd.Dir = r.workspace
		out, err := cmd.CombinedOutput()
		if err != nil && !isExitCode(err, 1) {
			return "", fmt.Errorf("search: %w: %s", err, out)
		}
		return string(out), nil
	}
	r.tools["apply_patch"] = func(ctx context.Context, input map[string]interface{}) (string, error) {
		patch, _ := input["patch"].(string)
		if strings.TrimSpace(patch) == "" {
			return "", fmt.Errorf("missing patch")
		}
		if strings.Contains(patch, "../") || strings.Contains(patch, "\\..\\") {
			return "", fmt.Errorf("patch paths must stay inside the workspace")
		}
		cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "-")
		cmd.Dir = r.workspace
		cmd.Stdin = strings.NewReader(normalizePatch(patch))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("apply patch: %w: %s", err, out)
		}
		return "Patch applied.", nil
	}
	r.tools["run_check"] = func(ctx context.Context, input map[string]interface{}) (string, error) {
		check, _ := input["check"].(string)
		args, ok := safeCheckArgs(check)
		if !ok {
			return "", fmt.Errorf("unsupported check %q", check)
		}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = r.workspace
		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("%s failed: %w", check, err)
		}
		return string(out), nil
	}
}

func (r *CodingToolRegistry) pathInput(input map[string]interface{}, key, fallback string) (string, error) {
	raw, _ := input[key].(string)
	if raw == "" {
		raw = fallback
	}
	if raw == "" {
		return "", fmt.Errorf("missing %s", key)
	}
	path := raw
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.workspace, path)
	}
	clean := filepath.Clean(path)
	if clean != r.workspace && !strings.HasPrefix(clean, r.workspace+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		if resolved != r.workspace && !strings.HasPrefix(resolved, r.workspace+string(os.PathSeparator)) {
			return "", fmt.Errorf("path resolves outside workspace")
		}
		clean = resolved
	}
	return clean, nil
}

func safeCheckArgs(check string) ([]string, bool) {
	checks := map[string][]string{
		"go_test": {"go", "test", "./..."}, "go_vet": {"go", "vet", "./..."}, "go_build": {"go", "build", "./..."},
		"npm_test": {"npm", "test"}, "npm_run_build": {"npm", "run", "build"}, "npm_run_lint": {"npm", "run", "lint"},
	}
	args, ok := checks[check]
	return args, ok
}

func normalizePatch(patch string) string {
	return strings.TrimSpace(patch) + "\n"
}

func codingToolTimeout() time.Duration {
	return time.Duration(envInt("CODING_AGENT_TOOL_TIMEOUT_SECONDS", 60)) * time.Second
}
func codingReadLimit() int64 { return int64(envInt("CODING_AGENT_MAX_FILE_BYTES", 256000)) }
func truncateToolOutput(s string) string {
	limit := envInt("CODING_AGENT_MAX_TOOL_OUTPUT_CHARS", 12000)
	if len(s) > limit {
		return s[:limit] + "\n... (truncated)"
	}
	return s
}
func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}

func envInt(name string, fallback int) int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && value > 0 {
		return value
	}
	return fallback
}
