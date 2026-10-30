package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"council-of-reeds/internal/hub"
	"council-of-reeds/internal/mcp"
	"council-of-reeds/internal/store"
	"council-of-reeds/internal/types"
	"council-of-reeds/internal/ui"
)

func main() {
	port := flag.Int("port", 8765, "Port for Council daemon and Watcher Lounge")
	noBrowser := flag.Bool("no-browser", false, "Do not auto-open browser on startup")
	dataDir := flag.String("data-dir", "./data", "Directory for persistent council ledger")
	flag.Parse()

	fmt.Println("==================================================")
	fmt.Println("             THE COUNCIL OF REEDS                 ")
	fmt.Println("         Interdimensional Chamber Relay           ")
	fmt.Println("==================================================")

	// 1. Initialize persistent store
	s, err := store.NewStore(*dataDir)
	if err != nil {
		fmt.Printf("[FATAL] Failed to initialize store: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize Hub & MCP server
	h := hub.NewHub(s)
	mcpServer := mcp.NewServer(h, s)

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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"universes":       universes,
			"active_variants": active,
		})
	})

	// API for Reed Prime Gavel Decree
	mux.HandleFunc("/api/decree", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Decree string `json:"decree"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Decree == "" {
			http.Error(w, "Invalid decree", http.StatusBadRequest)
			return
		}

		decreeMsg := &types.Message{
			FromUniverse: "earth-prime",
			FromVariant:  "Reed Prime (The Watcher)",
			ToUniverse:   "@council",
			Type:         types.MsgRuling,
			Status:       types.StatusDelivered,
			Topic:        "Executive Decree",
			Content:      payload.Decree,
			CreatedAt:    time.Now().UTC(),
		}
		_, _ = h.PostMessage(decreeMsg)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "DECREED"})
	})

	// Embedded Web UI
	mux.Handle("/", http.FileServer(ui.GetFileSystem()))

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		fmt.Printf("[RELAY ONLINE] Listening at http://%s\n", addr)
		fmt.Printf("[MCP ENDPOINT] http://%s/mcp\n", addr)
		fmt.Printf("[WATCHER UI ] http://%s/\n", addr)

		if !*noBrowser {
			time.Sleep(300 * time.Millisecond)
			openBrowser(fmt.Sprintf("http://%s/", addr))
		}

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[FATAL] HTTP server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on Interrupt / SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n[RELAY SHUTTING DOWN] Persisting ledger and closing frequencies...")
	_ = server.Close()
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

