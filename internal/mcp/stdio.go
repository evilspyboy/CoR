package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"
)

// RunSmartStdio checks if the central Council daemon is running on port 8765.
// If running, it proxies stdio JSON-RPC calls to http://127.0.0.1:8765/mcp so all agents share the same live hub.
// If not running, it runs the local server directly.
func (s *Server) RunSmartStdio(daemonURL string) error {
	client := &http.Client{Timeout: 30 * time.Second}

	// Probe daemon
	resp, err := client.Get(daemonURL + "/api/state")
	daemonRunning := (err == nil && resp.StatusCode == http.StatusOK)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if daemonRunning {
		// Proxy mode to central daemon
		return s.proxyStdioToHTTP(os.Stdin, os.Stdout, daemonURL+"/mcp", client)
	}

	// Local mode fallback
	return s.ServeStdio(os.Stdin, os.Stdout)
}

// ServeStdio runs the MCP JSON-RPC protocol locally over reader/writer
func (s *Server) ServeStdio(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 4*1024*1024)

	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			resp := rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "Parse error: " + err.Error()},
			}
			_ = encoder.Encode(resp)
			continue
		}

		resp := s.handleRequest(&req)
		_ = encoder.Encode(resp)
	}

	return scanner.Err()
}

func (s *Server) proxyStdioToHTTP(in io.Reader, out io.Writer, targetURL string, client *http.Client) error {
	scanner := bufio.NewScanner(in)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		httpResp, err := client.Post(targetURL, "application/json", bytes.NewReader(line))
		if err != nil {
			errResp := rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32603, Message: "Council daemon bridge error: " + err.Error()},
			}
			data, _ := json.Marshal(errResp)
			_, _ = out.Write(append(data, '\n'))
			continue
		}

		body, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()

		_, _ = out.Write(append(bytes.TrimSpace(body), '\n'))
	}

	return scanner.Err()
}
