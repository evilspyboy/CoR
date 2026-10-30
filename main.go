package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"council-of-reeds/internal/hub"
	"council-of-reeds/internal/mcp"
	"council-of-reeds/internal/store"
	"council-of-reeds/internal/types"
	"council-of-reeds/internal/ui"
)

func main() {
	exePath, _ := os.Executable()
	defaultDataDir := filepath.Join(filepath.Dir(exePath), "data")

	port := flag.Int("port", 8765, "Port for Council daemon and Watcher Lounge")
	noBrowser := flag.Bool("no-browser", false, "Do not auto-open browser on startup")
	dataDir := flag.String("data-dir", defaultDataDir, "Directory for persistent council ledger")
	mcpStdio := flag.Bool("mcp", false, "Run in stdio mode as an MCP server for Antigravity/Claude")
	flag.BoolVar(mcpStdio, "stdio", false, "Alias for --mcp")
	flag.Parse()

	// 1. Initialize persistent store
	s, err := store.NewStore(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Failed to initialize store: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize Hub & MCP server
	h := hub.NewHub(s)
	mcpServer := mcp.NewServer(h, s)

	// If invoked with --mcp / --stdio, run smart stdio bridge
	if *mcpStdio {
		daemonURL := fmt.Sprintf("http://127.0.0.1:%d", *port)
		if err := mcpServer.RunSmartStdio(daemonURL); err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "[ERROR] stdio server exited: %v\n", err)
		}
		return
	}

	// In Windows GUI mode, stdout/stderr have invalid handles; redirect them to council.log
	if runtime.GOOS == "windows" {
		logFile, err := os.OpenFile(filepath.Join(*dataDir, "council.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			os.Stdout = logFile
			os.Stderr = logFile
		}
	}

	fmt.Println("==================================================")
	fmt.Println("             THE COUNCIL OF REEDS                 ")
	fmt.Println("         Interdimensional Chamber Relay           ")
	fmt.Println("==================================================")

	// 3. Setup HTTP router
	mux := http.NewServeMux()

	// MCP JSON-RPC Gateway
	mux.Handle("/mcp", mcpServer)
	mux.Handle("/mcp/", mcpServer)

	// Server-Sent Events (SSE) stream for Watcher Lounge
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		watcherID := fmt.Sprintf("watcher-%d", time.Now().UnixNano())
		ch := h.SubscribeWatcher(watcherID)
		defer h.UnsubscribeWatcher(watcherID)

		// Send initial heartbeat
		fmt.Fprintf(w, ": heartbeat\n\n")
		flusher.Flush()

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	})

	// API for Watcher Lounge State
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		universes := s.GetAllPassports()
		active := h.GetActiveVariants()
		messages := s.GetRecentMessages(50)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"universes":       universes,
			"active_variants": active,
			"messages":        messages,
		})
	})

	// API for Reed Prime Gavel Decree
	mux.HandleFunc("/api/decree", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Decree     string `json:"decree"`
			ToUniverse string `json:"to_universe"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Decree == "" {
			http.Error(w, "Invalid decree", http.StatusBadRequest)
			return
		}

		text := strings.TrimSpace(payload.Decree)

		decreeMsg := &types.Message{
			FromUniverse: "earth-prime",
			FromVariant:  "Prime",
			ToUniverse:   payload.ToUniverse,
			Type:         types.MsgRuling,
			Status:       types.StatusDelivered,
			Topic:        "Executive Decree",
			Content:      text,
			CreatedAt:    time.Now().UTC(),
		}
		_, _ = h.PostMessage(decreeMsg)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "DECREED"})
	})

	// API for Muting/Unmuting a Variant by Reed Prime
	mux.HandleFunc("/api/mute", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			VariantTag string `json:"variant_tag"`
			Muted      bool   `json:"muted"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.VariantTag == "" {
			http.Error(w, "Invalid mute payload", http.StatusBadRequest)
			return
		}

		if err := h.SetVariantMuted(payload.VariantTag, payload.Muted); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "OK",
			"variant_tag": payload.VariantTag,
			"muted":       payload.Muted,
		})
	})

	// API for Graceful Relay Shutdown from Watcher Lounge
	mux.HandleFunc("/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "SHUTTING_DOWN"})
		go func() {
			time.Sleep(300 * time.Millisecond)
			os.Exit(0)
		}()
	})

	// Embedded Web UI
	fileServer := http.FileServer(ui.GetFileSystem())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else if strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		} else if strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		fileServer.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	fmt.Printf("[RELAY ONLINE] Listening at http://%s\n", addr)
	fmt.Printf("[MCP ENDPOINT] http://%s/mcp\n", addr)
	fmt.Printf("[WATCHER UI ] http://%s/\n", addr)

	if !*noBrowser {
		time.Sleep(300 * time.Millisecond)
		openBrowser(fmt.Sprintf("http://%s/", addr))
	}

	defer func() {
		if r := recover(); r != nil {
			_ = os.WriteFile("relay_crash.log", []byte(fmt.Sprintf("panic: %v", r)), 0644)
		}
	}()

	err = server.ListenAndServe()
	_ = os.WriteFile("relay_crash.log", []byte(fmt.Sprintf("listen returned: %v", err)), 0644)
	fmt.Println("[COUNCIL DISMISSED] Goodbye, Prime.")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

