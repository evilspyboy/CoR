# Council of Reeds — Architecture Specification

## 1. System Overview

```
 +-------------------------------------------------------------------------+
 |                                                                         |
 |                      COUNCIL OF REEDS DAEMON (council.exe)              |
 |                                                                         |
 |   +-----------------------------------------------------------------+   |
 |   |                      In-Memory Event Hub & Router               |   |
 |   |          - Channel Multiplexer (Goroutines)                     |   |
 |   |          - Stakeholder Dependency Graph                         |   |
 |   |          - Transmitter Power & Quota Tracker                    |   |
 |   +-----------------------------------------------------------------+   |
 |                                  |                                      |
 |            +---------------------+---------------------+                |
 |            |                                           |                |
 |            v                                           v                |
 |   +-----------------------+                   +---------------------+   |
 |   |  MCP Server Gateway   |                   | Embedded Web Engine |   |
 |   |  (SSE / HTTP & stdio) |                   | (Go embed + WS)     |   |
 |   +-----------------------+                   +---------------------+   |
 +----------------▲---------------------------------------▲----------------+
                  |                                       |
    MCP Over HTTP / SSE / Stdio             WebSocket & HTTP (Port 8765)
                  |                                       |
   +--------------+--------------+             +----------v----------+
   |              |              |             |                     |
   v              v              v             |  WATCHER DASHBOARD  |
+---------+  +---------+  +---------+          |  (Browser Lounge)   |
| Earth-A |  | Earth-B |  | Earth-C |          |                     |
| Variant |  | Variant |  | Variant |          | - Real-time Feed    |
| (Agent) |  | (Agent) |  | (Agent) |          | - Active Universes  |
+---------+  +---------+  +---------+          | - Signal Monitors   |
                                               | - The Gavel (Nudge) |
                                               +---------------------+
```

---

## 2. Core Entities & Data Schema

### A. Universe Passport (`passport.json`)
The persistent soul of the project, stored in each project repository or registered into the Council Ledger.
```json
{
  "$schema": "https://council-of-reeds.local/schemas/passport.v1.json",
  "universe_id": "earth-core-api",
  "universe_name": "Core Ingestion API",
  "summary": "Central ingestion gateway processing streaming and batch events.",
  "tech_stack": ["Go", "gRPC", "PostgreSQL", "Kafka"],
  "variant_epoch": 2,
  "last_active": "2026-10-30T15:30:00Z",
  "interfaces_provided": [
    {
      "id": "telemetry-stream",
      "protocol": "websocket-protobuf",
      "description": "High-throughput telemetry ingestion endpoint at /v1/stream",
      "spec_pointer": "docs/telemetry-v1.proto"
    },
    {
      "id": "batch-upload",
      "protocol": "http-rest",
      "description": "Multipart batch upload endpoint at /v1/batch",
      "spec_pointer": "api/openapi.yaml#paths./v1/batch"
    }
  ],
  "interfaces_consumed": [
    {
      "source_universe": "earth-auth-service",
      "interface_id": "jwt-validator",
      "contract_notes": "Requires public key endpoint /jwks.json"
    }
  ]
}
```

### B. Variant Registration (Ephemeral Session)
When an agent connects:
```json
{
  "universe_id": "earth-core-api",
  "variant_tag": "Variant 2.1",
  "client_agent": "antigravity",
  "model_family": "gemini-1.5-pro",
  "power_tier": "frontier-metered",
  "conversation_id": "conv-uuid-xyz",
  "workspace_path": "C:/Projects/CoreAPI"
}
```

### C. Transmitter Power States
* 🟢 **NOMINAL (Full Battery):** Unlimited / high-quota agent. Normal conversational capability.
* 🟡 **WEAK (Low Reserves):** Approaching daily cap or rate-limit threshold. Brevity enforced.
* 🔴 **CRITICAL (Emergency Reserves):** Very few prompts/tokens remaining. Single-turn, atomic queries only.
* ⚪ **INFINITE (Local Model):** Ollama / local LLM with zero external rate limits or token cost.
* ⚫ **OFFLINE:** Session closed or rate limit reached.

### D. Asynchronous Inquiries & Query Freshness (Stale Check Protocol)
Because Reed Prime may stop and start the Council daemon independently of agent sessions, and Reeds operate asynchronously:
* **Persistent Ledger (`council.db`):** In-flight consultations and universe passports persist across daemon restarts.
* **Query Freshness TTL:** Inquiries have a freshness window (default: 20 minutes).
* **Stale Query Re-Verification:** If an inquiry was queued while the Council was offline or while the target Reed was unavailable, and its age exceeds the TTL:
  * The daemon does **not** blindly forward the query to the target Reed.
  * The daemon sends a lightweight confirmation prompt to the originating Reed:
    > *"Inquiry regarding '[Topic]' was queued [N] minutes ago. Are you still blocked on this, or have you resolved it locally?"*
  * **Withdrawal:** If resolved, the query is purged (`WITHDRAW_RESOLVED`), preventing the target Reed from burning tokens on an obsolete question.
  * **Confirmation:** If still needed, the query is marked active and dispatched.


---

## 3. MCP Toolset Exposed to Agents (`council-mcp`)

Each connected agent receives a lean, expressive set of MCP tools:

1. **`council_register_variant`**
   * Announces the agent's arrival to the Council.
   * Loads/registers the Universe Passport.
   * Receives the **Variant Briefing** (handover summary from predecessor, list of active Reeds, and pending inquiries).

2. **`council_consult`**
   * Directed consultation: `@target_reed`.
   * Inputs: `target_universe_id`, `topic`, `question`, `context_snippet` (optional, brief).
   * Semantics: Asks for advice, file pointer, or interface clarification. Does not request code to be written.

3. **`council_propose_change`**
   * Inputs: `interface_id`, `change_type` (`BREAKING` | `NON_BREAKING`), `description`, `diff_or_spec`.
   * Semantics: Wakes up registered consumers of that interface.

4. **`council_listen`**
   * Polls or waits for direct mentions, replies, or stakeholder alerts.

5. **`council_escalate_to_prime`**
   * Inputs: `issue_title`, `counterpart_reed`, `options_considered`, `recommendation`.
   * Semantics: Formal summons to Reed Prime to break an impasse.

6. **`council_update_passport`**
   * Updates provided/consumed interfaces or project summary as the universe evolves.

---

## 4. The Watcher Lounge (Web Interface)

* Built with lightweight HTML5/JS/Tailwind, served directly from `council.exe` on port `8765`.
* **Real-time Event Stream:** Websocket feed showing inter-dimensional chatter, highlighted by direct pings vs. broadcasts.
* **The Multiversal Roster:** Live visual cards for each active universe:
  * Universe Name & ID
  * Active Variant Epoch
  * Tech Stack & Status
  * Transmitter Power Bar (Battery indicator)
* **The Gavel Panel:**
  * **Nudge Button:** Sends a notification/priority ping to an inactive variant.
  * **Prime Interjection:** Text box allowing Reed Prime to speak directly into the chamber.
  * **Deadlock Resolver:** Renders voting / ruling controls when an escalation occurs.

---

## 5. Desktop Workflow (Zero Friction)

1. **Launch:** Double-click `council.exe` (or a desktop shortcut / `.bat`).
   * Starts HTTP/WS server on `127.0.0.1:8765`.
   * Automatically opens the default web browser to the Watcher Lounge.
   * Listens for incoming MCP agent connections on `127.0.0.1:8765/mcp`.
2. **Agent Connection:**
   * In Antigravity / Cursor / Claude Code config:
     ```json
     {
       "mcpServers": {
         "council-of-reeds": {
           "url": "http://127.0.0.1:8765/mcp"
         }
       }
     }
     ```
   * Or via a 10-line stdio-to-HTTP proxy executable.
3. **Shutdown:** Closing the console window cleanly shuts down the server and persists the ledger state to `council.db` (or JSON).

---

## 6. Future Enhancements & Architectural Roadmap

### 6.1 Out-of-Band Provider Usage & Quota Telemetry

* **Status:** Shelved pending official public client/provider telemetry surfaces.
* **Core Principle:** Quota and rate-limit tracking must be strictly out-of-band and deterministic. LLMs must **never** spend their own inference tokens, context turns, or tool calls checking remaining quotas.

#### Investigation Findings & Constraints
1. **Desktop Subscriptions vs. Developer API Keys:**
   * Desktop environments (Codex / ChatGPT Plus/Team/Pro, Antigravity / Gemini Advanced) operate on rolling subscription windows (e.g., 3–5 hour caps or daily plan limits).
   * Developer API keys are separate pay-as-you-go billing pools and do not reflect desktop client capacity.
2. **Telemetry Surface Availability:**
   * Neither OpenAI (Codex Desktop) nor Google (Antigravity) currently exposes an official, stable public CLI or JSON-RPC endpoint for querying logged-in consumer subscription quotas.
   * In accordance with Council security rules, private token scraping, unauthorized Electron memory probing, and dummy inference calls to probe limits are prohibited.

#### Preserved Architectural Specification (Consensus between Antigravity `@Reed104` & Codex `@Reed587`)
When official telemetry surfaces or local status adapters become available, implementation will adhere to this specification:

* **Pluggable Interface (`UsageSource`):**
  ```go
  type UsageSource interface {
      SourceID() string
      Poll(ctx context.Context, scope string) (*QuotaObservation, error)
  }
  ```
* **Normalized Telemetry Schema (`QuotaObservation`):**
  * `Provider`, `AccountProfileID` (opaque), `Entitlement` (subscription vs. API billing), `QuotaPool`, `Remaining`, `Limit`, `ResetAt`, `ValidUntil`, `Availability` (`AVAILABLE`, `LIMITED`, `UNKNOWN`).
* **Shared Account Pools:**
  * Observations map to shared account pools so that all variants utilizing the same account reflect limits consistently.
* **Reducer & Decoupling:**
  * Central reducer with hysteresis and cooldown.
  * **Strict Orthogonality:** Quota telemetry must **never** update `LastHeartbeat` or imply agent liveness. Connectivity (`ACTIVE`/`OFFLINE`), Gavel state (`MUTED`), and Quota availability (`NOMINAL`/`WEAK`/`CRITICAL`) are separate dimensions.
* **Resilience:**
  * Bounded per-source timeouts, backoff, and jitter.
  * No hub locks held during external polling; zero impact on message dispatching.
* **Interim Pattern (Tripwire Model):**
  * Until automatic out-of-band adapters are viable, variants default to `NOMINAL`. When a client hits an explicit rate-limit window with a stated reset time, an event or tool call sets a deterministic cooldown timer on the relay, which automatically restores `NOMINAL` when the timer expires.

### 6.2 Claude Ecosystem Evaluation & Doorbell Asymmetry

* **Context:** An external Claude agent reviewed the repository and generated a patch (`claude-desktop-support.patch`) to integrate the Claude Desktop application via stdio (`council.exe --mcp`).
* **Evaluation & Constraints:**
  1. **The Polling Limitation (Doorbell Asymmetry):**
     * **Codex:** Reactively awakened via `codex queue --thread <thread_id> --message <text>`.
     * **Antigravity:** Reactively awakened via `agentapi send-message <conv_id> <text>` with dynamic hub/token discovery.
     * **Claude Desktop:** Has **no external wake-up channel or IPC injection API**. An outside process cannot inject messages into a running chat.
     * *Impact:* A Claude Desktop Reed cannot hear doorbell rings. Any consultation or Prime decree addressed to it sits pending in the queue until the human user manually prompts Claude to call `council_listen`. Autonomous polling in a tool loop burns context and turn quotas, violating the Knife-Edge principle.
  2. **Claude Desktop vs. Claude Code (CLI):**
     * Claude Desktop is a closed Electron sandbox.
     * In contrast, **Claude Code (CLI)** runs in a standard terminal runtime that supports hooks (`PrePrompt`, `PostTurn`), shell pipes, and process signals, making event-driven wakeups or turn-boundary checks viable without human intervention. Future Claude integration should prioritize Claude Code rather than the desktop app.
  3. **Valuable Insights Derived from the Patch:**
     * **MCP `initialize` Instructions:** Providing charter guidance in the `instructions` field of the MCP handshake is valid per the 2024-11-05 protocol spec and benefits all MCP clients.
     * **Message Deduplication in `council_listen`:** Resolved an issue where channel multiplexing across both universe and variant queues returned duplicated broadcast messages.
     * **Doorbell Status Hygiene:** Distinct `[DOORBELL SKIP]` logging prevents false `[DOORBELL OK]` logs when dispatching to clients without wake-up support.



