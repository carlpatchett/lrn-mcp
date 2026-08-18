# lrn-mcp (`lrn-mcp`)

A lightweight, local **Learning & Spaced Repetition MCP Server** with companion CLI (`lrn`).

When AI agents or developers run terminal commands (`kubectl`, `git`, `jj`, `docker`, `bash`, `terraform`), `lrn-mcp` captures patterns, explains command anatomy, flags, and safety implications, and schedules interactive active recall sessions using a modified **SM-2 spaced repetition algorithm** backed by local SQLite.

---

## Key Features

1. **Command Anatomy & Explanation Engine**:
   - Decomposes commands into base tools, subcommands, flags, values, and positional arguments.
   - Explains flag behaviors and safety classifications (`read_only`, `mutating`, `destructive`, `privileged`).
   - Normalizes command syntax into canonical templates (e.g. `kubectl get pods -n <NAMESPACE> -l <SELECTOR>`).

2. **Interactive Learning & Spaced Repetition (SM-2)**:
   - Tracks repetitions, ease factors, intervals, and mastery scores ($0.0 - 1.0$) locally in SQLite (`~/.config/lrn/learning.db`).
   - Generates flashcards and interactive recall prompts for due or unfamiliar commands.

3. **Approval Gating & Snooze Controls**:
   - `learn_exec` returns command breakdowns, pro-tips/hints, and learning challenges along with approval tokens to prevent unintended automatic execution.
   - Snooze prompts by tool, pattern, or globally for $N$ minutes/hours.
   - Permanently mark familiar patterns as `known` to exclude them from reviews.

4. **MCP Protocol & Companion CLI**:
   - **MCP stdio server** (`bin/lrn-mcp`): Exposes tools, resources, and prompts over standard JSON-RPC 2.0.
   - **CLI Companion** (`bin/lrn`): Run revision quizzes, inspect stats, explain commands, and manage snooze/known rules directly from the terminal.

---

## Installation & Build

```bash
cd lrn-mcp
make build

# Optional: install binaries to ~/.local/bin
make install
```

---

## MCP Client Configuration

Add `lrn-mcp` to your MCP configuration (e.g. `~/.claude.json` or `~/.omp/agent/mcp.json`):

```json
{
  "mcpServers": {
    "lrn-mcp": {
      "command": "~/.local/bin/lrn-mcp",
      "args": []
    }
  }
}
```

## MCP Server Surface

### Tools
- **`explain_command`**: Deconstructs and explains the anatomy, flags, and safety implications of a command.
- **`learn_exec`**: Evaluates commands against spaced repetition rules. Returns breakdown and challenge tokens, requiring explicit user completion or approval tokens.
- **`snooze_learning`**: Temporarily pauses learning prompts (e.g., during outages or emergencies).
- **`mark_known`**: Permanently marks a command or pattern as known.
- **`get_due_reviews`**: Fetches due spaced repetition flashcards.
- **`submit_review_score`**: Submits a recall quality score ($0-5$) to update the SM-2 schedule.
- **`get_learning_stats`**: Retrieves summary learning statistics.

### Resources
- `learning://stats/overview`: Real-time JSON summary of tracked patterns, due reviews, and mastery levels.
- `learning://config/exclusions`: Active snooze rules and permanently excluded command patterns.

### Prompts
- `spaced_review_session`: Launches an interactive spaced repetition review session with the LLM.

---

## CLI Usage (`lrn`)

```bash
# Show mastery statistics and review counts
lrn stats

# Explain command anatomy and flags
lrn explain "kubectl get pods -n prod -l app=frontend -o wide"

# Launch an interactive terminal flashcard quiz
lrn review

# Filter review by specific tool
lrn review kubectl

# Snooze learning prompts for 60 minutes
lrn snooze tool kubectl 60 "Debugging production issue"

# Mark a command as permanently known
lrn known exact_command "git status" "Standard command"
```
