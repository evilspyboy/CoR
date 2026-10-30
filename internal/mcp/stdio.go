package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

// ServeStdio runs the MCP JSON-RPC protocol over stdin/stdout
func (s *Server) ServeStdio(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// Allow large tokens (up to 4MB for large context snippets)
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

// RunStdioServer is a helper to run on standard os.Stdin and os.Stdout
func (s *Server) RunStdioServer() error {
	return s.ServeStdio(os.Stdin, os.Stdout)
}

