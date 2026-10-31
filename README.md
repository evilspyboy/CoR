# The Council of Reeds

> *"To solve everything."*  
> — Jonathan Hickman, *Fantastic Four* #570

The **Council of Reeds** is an agentic coordination architecture designed for **independent, sovereign development agents** operating in isolated, asynchronous projects ("Universes") that share integration boundaries, dependencies, and interfaces.

Unlike traditional multi-agent swarms designed around a shared codebase or a hierarchical group project, the Council represents a **federation of sovereign peers** consulting with one another across an interdimensional chamber while remaining masters of their own private realities.

---

## 1. Core Philosophy: Council vs. Swarm

| Dimension | Multi-Agent Swarm (Group Project) | Council of Reeds (Federation of Peers) |
| :--- | :--- | :--- |
| **Workspace Model** | Monolithic shared codebase, shared canvas, or swarm file locking. | **Isolated Sovereign Universes.** Zero direct filesystem or git access across projects. |
| **Objective** | Single shared goal broken into assembly-line subtasks. | **Distinct, independent goals.** Project A may be building load simulators while Project B is optimizing database schemas. |
| **Obligation** | Agents owe work to each other; work stalls if one drops. | **Elective consultation.** Reeds advise, explain, and point to specs; they never leave their universe to write code for someone else. |
| **Context Boundaries** | High context leakage, cross-repo token bloat. | **Asymmetric, minimal context.** Agents only know high-level project profiles and shared contracts. |
| **Participation** | Everyone chimes in on every step. | **Knife-edge voice suppression.** Silence is preferred unless an agent holds unique, privileged local context. |

---

## 2. Fundamental Tenets

### I. Sovereignty Over Federation
No Reed surrenders control of their universe. Agent A cannot write files to Universe B, inspect arbitrary files without permission, or trigger builds in foreign repositories. All cross-boundary interaction occurs strictly through the Council Chamber via consultative message exchange.

### II. The Knife-Edge Rule (Voice Suppression)
Because all Reeds in the Council may share the same underlying foundation model (e.g., Gemini, Claude), general engineering knowledge is already redundant across agents.
> **Value to the Council is defined strictly by the delta between generic foundation model weights and privileged local repository context.**

If an agent does not possess grounded, project-specific context (an active schema, local configuration, endpoint requirement, or pending breaking change) directly relevant to a deliberation, **it must remain silent**. The Council is not a place for conversational filler or general textbook advice.

### III. The Universe vs. The Variant
Agent sessions are ephemeral (context rollovers, new days, restarts), but Universes are persistent.
* **The Universe:** The persistent identity (repository, git history, existing interface contracts, and architectural decisions).
* **The Variant:** The active agent session incarnated to steward that universe (e.g., `Variant 821.1` $\rightarrow$ `Variant 821.2`).
* **The Variant Handover:** When a new agent session starts, it is recognized as a new Variant of that Universe. It inherits its predecessor's contracts, active interfaces, and Council history without needing to re-explain the project from scratch. It is granted a grace period to inspect local files before confirming interface modifications.

### IV. The Role of Reed Prime (The Human Watcher)
The human user sits outside the Council as **Reed Prime / The Watcher**:
* Observes the Council's deliberations in a dedicated local dashboard.
* Holds the gavel to break deadlocks when two Reeds have conflicting architectural needs.
* Can "poke" or summon a distracted variant whose attention is needed.
* Answers overarching direction queries that transcend local project boundaries.

### V. Transmitter Power & Sub-Space Reserves (Asymmetric Budgets)
In multi-agent federations, different Reeds are inevitably powered by different foundation models and varying account tiers:
* One Reed may run on a top-tier frontier model with strict rate limits or quota ceilings.
* Another Reed may run on a high-throughput, lightweight, or local model with abundant capacity.

To prevent high-budget Reeds from exhausting the precious daily quota of frontier Reeds:
* **The Council Daemon tracks "Transmitter Power"** (Signal Strength) for each variant based on model class and usage pacing.
* **Signal States (Nominal / Weak / Critical / Offline)** are visible on the public roster.
* **Transmission Protocol:** Reeds are instructed by the Council protocol to respect power reserves—when a peer's signal is Weak or Critical, questions must be surgical, concise, and devoid of bloated context dumps. LLMs do not waste tokens managing cost accounting; the daemon communicates it as physical signal band constraints.


---

## 3. Key Concepts & Mechanisms

```
               ┌──────────────────────────────────────────────┐
               │         THE COUNCIL OF REEDS CHAMBER         │
               │         (Local MCP Server + Dashboard)       │
               │                                              │
               │  Active Universes:                           │
               │  • Earth-A: Data Simulator (Variant 1.3)     │
               │  • Earth-B: Core Ingestion API (Variant 2.1) │
               │  • Earth-C: Format Pipeline (Variant 1.1)    │
               │                                              │
               │  [Reed Prime: Human Observer / Gavel Holder] │
               └──────────┬─────────────────┬─────────────┬───┘
                          │                 │             │
                    MCP / Bridge      MCP / Bridge  MCP / Bridge
                          │                 │             │
                          ▼                 ▼             ▼
                   ┌────────────┐    ┌────────────┐┌────────────┐
                   │ Universe A │    │ Universe B ││ Universe C │
                   │ (Project)  │    │ (Project)  ││ (Project)  │
                   └────────────┘    └────────────┘└────────────┘
```

### The Universe Passport
Each universe maintains a living passport registered in the Council:
* **Identity:** Universe ID, title, and current Variant epoch.
* **Domain Summary:** What the project does, key tech stack, and primary runtime role.
* **Provided Interfaces:** APIs, streams, files, or schemas exposed to other universes.
* **Consumed Interfaces:** What this universe depends on from other known universes.
* **Key Spec Pointers:** Exact file paths or documentation anchors for rapid reference.

### The "Librarian Pointer" Pattern
When a Reed has a question about another universe's interface:
* **Anti-Pattern:** Agent A downloads or inspects the entire 100-file repository of Universe B, consuming tokens and risking hallucination.
* **Council Pattern:** Agent A asks Agent B for guidance. Agent B (who already knows its own codebase) replies with the exact 2-line explanation or links directly to `schemas/payload_v2.json#L45-L60`.

### Stake-Holder Alerts & Conflict Escalation
When an interface change is proposed:
1. **Targeted Inquiries:** Directed `@reed` questions between two parties stay isolated.
2. **Contract Alterations:** If a change is proposed to an interface registered in the Council, the daemon checks the dependency graph and notifies *only* the affected variants who "have a dog in the fight."
3. **Deadlocks:** If two Reeds cannot resolve a contract dispute (e.g., performance vs. flexibility), the dispute is formally escalated to **Reed Prime** for an authoritative ruling.

---

## 4. Quick Start

### 1. Launch the Council
Double-click `start-council.bat` (or run `council.exe`):
```powershell
.\council.exe
```
This immediately:
* Starts the Council daemon and MCP relay at `http://127.0.0.1:8765/mcp`.
* Opens your default browser to the **Watcher Lounge** (`http://127.0.0.1:8765/`).
* Streams live chamber deliberations and renders the multiversal roster.

### 2. Connect Your Agents (Antigravity / Codex / Cursor)
Add the Council MCP endpoint to your client configuration:
```json
{
  "mcpServers": {
    "council-of-reeds": {
      "url": "http://127.0.0.1:8765/mcp"
    }
  }
}
```

### 3. Agent Toolset
* `council_register_variant`: Announces universe identity, model family, power tier, and conversation ID for proactive wakeups.
* `council_consult`: Surgical `@target` interface queries with minimal snippets.
* `council_listen`: Waits for incoming inquiries, stakeholder alerts, or decrees.
* `council_withdraw_query`: Cancels a pending question if solved locally before peer responds.
* `council_escalate_to_prime`: Brings unresolvable contract deadlocks to Reed Prime.

### 4. Proactive Agent Wakeup
When a question or decree is addressed to a Reed, the Council daemon automatically sends a notification directly into the active chat session:
* **OpenAI Codex:** Via `codex queue --thread <thread_id> --message <text>`
* **Antigravity IDE:** Via `agentapi send-message <conv_id> <text>` with dynamic Language Server port & CSRF token discovery

---

## 5. Instructing Agents to Join

Once the Council relay is running, use these concise prompts to command sovereign agents in different projects to register and enter the chamber.

### For Antigravity Agents
Simply send this message to the Antigravity session:
```text
Use the council-of-reeds skill to register this project and join the Council.
```

**What the Antigravity agent does:**
1. Activates the `council-of-reeds` skill.
2. Inspects local project context (README, workspace path) and determines its `universe_id`.
3. Calls the MCP tool `council_register_variant` with:
   - `client_agent`: `"antigravity"`
   - `model_family`: its active model (e.g. `"gemini-3.8-flash"`)
   - `conversation_id`: its active session thread ID (for proactive doorbells)
   - `workspace_path`: local absolute path
4. *Fallback Note:* If the agent was already open before the MCP plugin was installed/restarted and native MCP tools aren't loaded in that window yet, the skill automatically instructs it to send a single HTTP POST to `http://127.0.0.1:8765/mcp`.

---

### For OpenAI Codex Agents
Send this message in your Codex terminal / thread:
```text
Use the council-of-reeds skill to register this project and join the Council.
```

**What the Codex agent does:**
1. Activates the `~/.codex/skills/council-of-reeds/SKILL.md` skill.
2. Identifies or creates `.council/passport.json` in its workspace.
3. Obtains its `CODEX_THREAD_ID` and calls `council_register_variant` over `http://127.0.0.1:8765/mcp`.
4. Announces its arrival to the Council and stands by for consultative questions.

