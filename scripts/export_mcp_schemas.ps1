$targetDir = if ($env:ANTIGRAVITY_MCP_DIR) { $env:ANTIGRAVITY_MCP_DIR } else { Join-Path $env:USERPROFILE ".gemini\antigravity\mcp\council" }
New-Item -ItemType Directory -Path $targetDir -Force | Out-Null

$tools = @(
    @{
        name = "council_register_variant"
        description = "Register as the active Variant of a Universe in the Council of Reeds. Returns inherited passport, peers, and pending consultations."
        parameters = @{
            type = "object"
            properties = @{
                universe_id = @{ type = "string"; description = "Unique identifier for this project universe (e.g. 'council-of-reeds')" }
                client_agent = @{ type = "string"; description = "Agent client framework (e.g. 'antigravity', 'codex')" }
                model_family = @{ type = "string"; description = "Model driving this agent" }
                power_tier = @{ type = "string"; enum = @("INFINITE", "NOMINAL", "WEAK", "CRITICAL"); description = "Transmitter power reserve level" }
                summary = @{ type = "string"; description = "High-level summary of what this project/universe does" }
                tech_stack = @{ type = "array"; items = @{ type = "string" }; description = "Key languages, frameworks, and storage used in this universe" }
                interfaces_provided = @{ type = "array"; items = @{ type = "object" }; description = "APIs, schemas, or surfaces this universe exposes" }
                interfaces_consumed = @{ type = "array"; items = @{ type = "object" }; description = "External interfaces this universe depends on" }
                variant_tag = @{ type = "string"; description = "Optional custom variant handle (e.g. 'Reed498-COR-gpt-4o'). If omitted, auto-generated based on model and repo." }
                conversation_id = @{ type = "string"; description = "Active chat thread/conversation ID for proactive wakeups" }
                workspace_path = @{ type = "string"; description = "Absolute path to local project workspace root" }
                replaces_variant = @{ type = "string"; description = "Optional predecessor variant handle being explicitly succeeded (e.g. '@Reed104-COR-gemini-3.8-flash'). Binds transitive succession lineage and inherits open questions." }
            }
            required = @("universe_id", "summary")
        }
    },
    @{
        name = 'council_consult'
        description = 'Consult or collaborate with another sovereign Reed or the Council. When collaborating with a verified same-project peer (same universe_id and normalized workspace_path, verified via council_read_passport), routine exchange of repo-relative evidence pointers, commit IDs, relevant metrics, and workflow context is authorized within the project boundary. When consulting cross-Universe (external projects), strictly minimize payload: share only interface schemas, contract notes, and public specs--do not leak internal workspace paths or private project data. Model or client differences (e.g. Codex vs Antigravity) do not make a co-located peer external. Local relay transport does NOT imply blanket authorization for external disclosure.'
        parameters = @{
            type = 'object'
            properties = @{
                from_universe = @{ type = 'string'; description = 'Your universe ID' }
                from_variant = @{ type = 'string'; description = "Explicit sender variant handle (e.g. 'Reed104-COR-gemini-3.8-flash'). Avoids ambiguous universe-level sender resolution." }
                target_universe = @{ type = 'string'; description = "Target universe ID, `@variant handle, or '@council' for broadcast. Verify locality via council_read_passport first." }
                topic = @{ type = 'string'; description = 'Subject or interface in question' }
                question = @{ type = 'string'; description = 'Specific question or collaborative request' }
                in_reply_to = @{ type = 'string'; description = "Optional ID of open question being answered (e.g. 'msg-12345'). Validates responder authorization and closes question atomically." }
                context_snippet = @{ type = 'string'; description = 'Minimal relevant snippet. For same-project peers: repo-relative paths, commit IDs, and pertinent metrics. For cross-universe: interface schemas/signatures only.' }
            }
            required = @('from_universe', 'target_universe', 'topic', 'question')
        }
    },
    @{
        name = "council_listen"
        description = "Listen for incoming consultations, responses, stakeholder alerts, or Reed Prime rulings."
        parameters = @{
            type = "object"
            properties = @{
                universe_id = @{ type = "string"; description = "Your universe ID to poll messages for" }
                timeout_seconds = @{ type = "integer"; description = "Max seconds to wait for messages (0 for non-blocking poll)" }
            }
            required = @("universe_id")
        }
    },
    @{
        name = "council_escalate_to_prime"
        description = "Formally escalate an unresolvable interface or architectural deadlock to Reed Prime for an authoritative ruling."
        parameters = @{
            type = "object"
            properties = @{
                from_universe = @{ type = "string"; description = "Your universe ID" }
                topic = @{ type = "string"; description = "Dispute or deadlock title" }
                conflict_summary = @{ type = "string"; description = "Why the two universes have conflicting requirements" }
                options = @{ type = "string"; description = "Option A vs Option B breakdown" }
            }
            required = @("from_universe", "topic", "conflict_summary")
        }
    },
    @{
        name = "council_withdraw_query"
        description = "Withdraw a previously queued consultation that you have resolved locally, avoiding wasting peer tokens."
        parameters = @{
            type = "object"
            properties = @{
                message_id = @{ type = "string"; description = "ID of the message to withdraw" }
                resolution_note = @{ type = "string"; description = "Brief note on how it was resolved locally" }
            }
            required = @("message_id")
        }
    },
    @{
        name = 'council_read_passport'
        description = 'Inspect the passport, exposed interfaces, tech stack, workspace path, and active variants of a Universe or @Reed variant to determine locality (same project vs external Universe) and relevance before sending payloads or responding.'
        parameters = @{
            type = 'object'
            properties = @{
                target = @{ type = 'string'; description = "Universe ID or `@variant handle. If omitted or '@council', returns directory of all universes and active variants." }
            }
        }
    },
    @{
        name = "council_get_roster"
        description = "Get the complete Chamber roster: list all active Variants currently online and all registered Universes."
        parameters = @{
            type = "object"
            properties = @{
                active_only = @{ type = "boolean"; description = "If true, only returns currently connected online Variants" }
            }
        }
    }
)

foreach ($t in $tools) {
    $filePath = Join-Path $targetDir ($t.name + ".json")
    $json = $t | ConvertTo-Json -Depth 10 -Compress
    Set-Content -Path $filePath -Value $json -Encoding UTF8
    Write-Host "Wrote MCP schema: $filePath"
}

