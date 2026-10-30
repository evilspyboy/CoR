package dispatcher

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"council-of-reeds/internal/types"
)

// Dispatcher manages proactive agent wakeup across different AI clients
type Dispatcher struct {
	cachedCodex    string
	cachedAgentAPI string
}

// NewDispatcher creates a new proactive notification dispatcher
func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// Notify proactively awakens an agent session with a message from the Council
func (d *Dispatcher) Notify(session *types.VariantSession, msg *types.Message) error {
	if session == nil || session.ConversationID == "" {
		return nil // No conversation ID registered, can only poll via MCP
	}

	client := strings.ToLower(session.ClientAgent)
	summary := fmt.Sprintf("[%s] %s: %s", msg.Type, msg.Topic, msg.Content)
	if msg.ContextSnippet != "" {
		summary += fmt.Sprintf(" | Snippet: %s", msg.ContextSnippet)
	}

	switch {
	case strings.Contains(client, "codex"):
		return d.notifyCodex(session.ConversationID, summary)
	case strings.Contains(client, "antigravity") || strings.Contains(client, "gemini"):
		return d.notifyAntigravity(session.ConversationID, session.WorkspacePath, summary)
	default:
		// Unknown client framework
		return nil
	}
}

// notifyCodex uses 'codex queue --thread <id> --message <text>'
func (d *Dispatcher) notifyCodex(threadID, message string) error {
	exe := d.resolveCodexExe()
	if exe == "" {
		return fmt.Errorf("codex executable not found")
	}

	cmd := exec.Command(exe, "queue", "--thread", threadID, "--message", message)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codex queue failed (%v): %s", err, string(output))
	}
	return nil
}

func (d *Dispatcher) resolveCodexExe() string {
	if d.cachedCodex != "" && fileExists(d.cachedCodex) {
		return d.cachedCodex
	}

	if p, err := exec.LookPath("codex"); err == nil {
		d.cachedCodex = p
		return p
	}

	home, err := os.UserHomeDir()
	if err == nil {
		pattern := filepath.Join(home, "AppData", "Local", "OpenAI", "Codex", "bin", "*", "codex.exe")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			// pick the latest
			d.cachedCodex = matches[len(matches)-1]
			return d.cachedCodex
		}
	}
	return ""
}

// notifyAntigravity uses 'agentapi.exe agentapi send-message <conv_id> <content>'
func (d *Dispatcher) notifyAntigravity(convID, workspacePath, message string) error {
	exe := d.resolveAgentAPIExe()
	if exe == "" {
		return fmt.Errorf("agentapi executable not found")
	}

	cleanMsg := strings.ReplaceAll(message, "\n", " ")
	cmd := exec.Command(exe, "agentapi", "send-message", convID, cleanMsg)

	// Set environment (IPC hook if present in scratch)
	env := os.Environ()
	if workspacePath != "" {
		hook := d.findIPCHook(workspacePath)
		if hook != "" {
			env = append(env, "VSCODE_IPC_HOOK="+hook)
		}
	}
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("agentapi send-message failed (%v): %s", err, string(output))
	}
	return nil
}

func (d *Dispatcher) resolveAgentAPIExe() string {
	if d.cachedAgentAPI != "" && fileExists(d.cachedAgentAPI) {
		return d.cachedAgentAPI
	}

	if envPath := os.Getenv("ANTIGRAVITY_AGENTAPI_EXE"); envPath != "" && fileExists(envPath) {
		d.cachedAgentAPI = envPath
		return envPath
	}

	if envPath := os.Getenv("ANTIGRAVITY_AGENTAPI_PATH"); envPath != "" && fileExists(envPath) {
		d.cachedAgentAPI = envPath
		return envPath
	}

	if p, err := exec.LookPath("agy.exe"); err == nil {
		d.cachedAgentAPI = p
		return p
	}

	if p, err := exec.LookPath("agentapi.exe"); err == nil {
		d.cachedAgentAPI = p
		return p
	}

	// Try common Antigravity / Gemini paths on Windows
	home, err := os.UserHomeDir()
	if err == nil {
		candidates := []string{
			filepath.Join(home, ".gemini", "bin", "agy.exe"),
			filepath.Join(home, ".gemini", "antigravity-ide", "scratch", "antigravity-orchestrator", "bin", "agentapi.exe"),
			filepath.Join(home, "AppData", "Local", "Programs", "Antigravity", "resources", "app", "bin", "agentapi.exe"),
		}
		for _, c := range candidates {
			if fileExists(c) {
				d.cachedAgentAPI = c
				return c
			}
		}
	}

	// If on Windows, query running processes for language_server
	if runtime.GOOS == "windows" {
		psScript := `Get-CimInstance Win32_Process -Filter "Name = 'language_server_windows_x64.exe'" | Select-Object -ExpandProperty CommandLine`
		cmd := exec.Command("powershell", "-NoProfile", "-Command", psScript)
		out, err := cmd.Output()
		if err == nil {
			re := regexp.MustCompile(`([A-Za-z]:\\[^"]+?\\language_server_windows_x64\.exe)`)
			match := re.FindStringSubmatch(string(out))
			if len(match) > 1 {
				dir := filepath.Dir(match[1])
				target := filepath.Join(dir, "agentapi.exe")
				if fileExists(target) {
					d.cachedAgentAPI = target
					return target
				}
			}
		}
	}

	return ""
}

func (d *Dispatcher) findIPCHook(workspacePath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	ipcPath := filepath.Join(home, ".gemini", "antigravity-ide", "scratch", "antigravity-orchestrator", "ipc_hooks.json")
	data, err := os.ReadFile(ipcPath)
	if err != nil {
		return ""
	}

	var hooks map[string]interface{}
	if err := json.Unmarshal(data, &hooks); err != nil {
		return ""
	}

	normalized := strings.ToLower(filepath.ToSlash(workspacePath))
	if entry, ok := hooks[normalized]; ok {
		if m, isMap := entry.(map[string]interface{}); isMap {
			if h, ok := m["ipc_hook"].(string); ok {
				return h
			}
		} else if s, isStr := entry.(string); isStr {
			return s
		}
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
