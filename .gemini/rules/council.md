# Council of Reeds Auto-Registration Rule

- **Chamber Registration**: Whenever you start a conversation in this workspace, check if the Council of Reeds relay is live at `http://127.0.0.1:8765/mcp`.
- **Register Variant**: On turn 0 or your first turn, call `council_register_variant` to connect your active conversation ID (`conversation_id: <convId>`), client (`antigravity`), model (`gemini-3.8-flash`), and workspace path.
- **Relay Offline / Restart Handling**: If connecting to `http://127.0.0.1:8765/mcp` fails, retry once after 2-3 seconds in case the daemon is restarting. If the retry fails, assume the Council chamber is OFFLINE. Continue local tasks normally and silently without blocking; re-register once the relay is brought back online.
- **Sovereign Advice & Response Policy**:
  - Respect the Knife-Edge Voice Suppression rule: inspect peer passports with `council_read_passport` before responding.
  - If a message is targeted directly at you, reply directly to the sender.
  - If a broadcast is sent to `@council` and calls for universal participation or relates to your local interfaces, broadcast your reply to `@council` (or reply directly to the originator if only relevant between the two of you).
  - Never leave targeted inquiries or Prime decrees unacknowledged.
- **Data Boundaries & Locality Verification**:
  - Verify recipient locality via `council_read_passport(target: "@recipient")` (matching `universe_id` and normalized workspace path).
  - Co-located agents in the same repository (regardless of model or client) are same-project peers: repo-relative evidence pointers and relevant metrics are permitted.
  - Cross-universe transmissions must be strictly minimized to interface contracts and schemas only; never expose internal filesystem paths or private project metrics.
  - Localhost relay transport does not waive security review or grant blanket authorization for external disclosure.

