package db

import (
	"database/sql"
	"fmt"
)

const schemaDDL = `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tools (
    id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 1,
    strictness TEXT NOT NULL DEFAULT 'standard',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS command_patterns (
    id TEXT PRIMARY KEY,
    tool_id TEXT NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    pattern_template TEXT NOT NULL,
    canonical_tokens TEXT NOT NULL,
    summary TEXT NOT NULL,
    safety_level TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS flag_definitions (
    id TEXT PRIMARY KEY,
    tool_id TEXT NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    subcommand TEXT,
    flag_name TEXT NOT NULL,
    flag_type TEXT NOT NULL,
    explanation TEXT NOT NULL,
    is_common INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS repetition_items (
    id TEXT PRIMARY KEY,
    item_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    repetitions INTEGER NOT NULL DEFAULT 0,
    interval_days REAL NOT NULL DEFAULT 0.0,
    ease_factor REAL NOT NULL DEFAULT 2.5,
    last_reviewed_at TIMESTAMP,
    next_review_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    mastery_score REAL NOT NULL DEFAULT 0.0,
    status TEXT NOT NULL DEFAULT 'learning'
);

CREATE TABLE IF NOT EXISTS execution_logs (
    id TEXT PRIMARY KEY,
    session_id TEXT,
    raw_command TEXT NOT NULL,
    pattern_id TEXT REFERENCES command_patterns(id),
    mode TEXT NOT NULL,
    is_interactive INTEGER NOT NULL DEFAULT 1,
    action_taken TEXT NOT NULL,
    quality_score INTEGER,
    response_time_ms INTEGER,
    executed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS snooze_rules (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    target_id TEXT,
    reason TEXT,
    snooze_until TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS known_exclusions (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    target TEXT NOT NULL,
    marked_reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_repetition_next_review ON repetition_items(next_review_at, status);
CREATE INDEX IF NOT EXISTS idx_snooze_lookup ON snooze_rules(scope, target_id, snooze_until);
CREATE INDEX IF NOT EXISTS idx_patterns_tool ON command_patterns(tool_id);
CREATE INDEX IF NOT EXISTS idx_flag_lookup ON flag_definitions(tool_id, subcommand, flag_name);
`

func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(schemaDDL); err != nil {
		return fmt.Errorf("failed executing schema ddl: %w", err)
	}

	// Seed default tools if empty
	defaultTools := []string{"kubectl", "git", "jj", "docker", "bash", "terraform"}
	for _, tool := range defaultTools {
		_, _ = db.Exec("INSERT OR IGNORE INTO tools (id, enabled, strictness) VALUES (?, 1, 'standard')", tool)
	}

	// Seed knowledge base of common flags
	seedFlags(db)

	return nil
}

func seedFlags(db *sql.DB) {
	flags := []FlagDefinition{
		// Kubectl
		{ID: "k8s_n", ToolID: "kubectl", Subcommand: "", FlagName: "-n", FlagType: "string", Explanation: "Specifies the target Kubernetes namespace for the operation.", IsCommon: true},
		{ID: "k8s_namespace", ToolID: "kubectl", Subcommand: "", FlagName: "--namespace", FlagType: "string", Explanation: "Specifies the target Kubernetes namespace for the operation.", IsCommon: true},
		{ID: "k8s_l", ToolID: "kubectl", Subcommand: "", FlagName: "-l", FlagType: "string", Explanation: "Selector (label query) to filter on, supports '=', '==', and '!='.", IsCommon: true},
		{ID: "k8s_selector", ToolID: "kubectl", Subcommand: "", FlagName: "--selector", FlagType: "string", Explanation: "Selector (label query) to filter on.", IsCommon: true},
		{ID: "k8s_o", ToolID: "kubectl", Subcommand: "", FlagName: "-o", FlagType: "string", Explanation: "Output format (e.g. yaml, json, wide, name, custom-columns).", IsCommon: true},
		{ID: "k8s_all_namespaces", ToolID: "kubectl", Subcommand: "", FlagName: "-A", FlagType: "boolean", Explanation: "List the requested object(s) across all namespaces.", IsCommon: true},
		{ID: "k8s_dry_run", ToolID: "kubectl", Subcommand: "", FlagName: "--dry-run", FlagType: "string", Explanation: "Simulates the command without persisting changes (client, server, or none).", IsCommon: true},
		{ID: "k8s_tail", ToolID: "kubectl", Subcommand: "logs", FlagName: "--tail", FlagType: "int", Explanation: "Lines of recent log file to display.", IsCommon: true},
		{ID: "k8s_follow", ToolID: "kubectl", Subcommand: "logs", FlagName: "-f", FlagType: "boolean", Explanation: "Follow/stream log output in real time.", IsCommon: true},

		// Git
		{ID: "git_p", ToolID: "git", Subcommand: "add", FlagName: "-p", FlagType: "boolean", Explanation: "Interactively choose hunks of patch between the index and the work tree.", IsCommon: true},
		{ID: "git_patch", ToolID: "git", Subcommand: "add", FlagName: "--patch", FlagType: "boolean", Explanation: "Interactively choose hunks of patch between the index and the work tree.", IsCommon: true},
		{ID: "git_amend", ToolID: "git", Subcommand: "commit", FlagName: "--amend", FlagType: "boolean", Explanation: "Replace the tip of the current branch by creating a new commit.", IsCommon: true},
		{ID: "git_no_edit", ToolID: "git", Subcommand: "commit", FlagName: "--no-edit", FlagType: "boolean", Explanation: "Use the selected commit message without launching an editor.", IsCommon: true},
		{ID: "git_rebase_i", ToolID: "git", Subcommand: "rebase", FlagName: "-i", FlagType: "boolean", Explanation: "Start an interactive rebase to reorder, squash, or edit commits.", IsCommon: true},
		{ID: "git_force_lease", ToolID: "git", Subcommand: "push", FlagName: "--force-with-lease", FlagType: "boolean", Explanation: "Refuses to update if the remote ref is not at the expected commit.", IsCommon: true},

		// Jujutsu (jj)
		{ID: "jj_r", ToolID: "jj", Subcommand: "", FlagName: "-r", FlagType: "string", Explanation: "Specifies the target revision/changeset revset expression.", IsCommon: true},
		{ID: "jj_revision", ToolID: "jj", Subcommand: "", FlagName: "--revision", FlagType: "string", Explanation: "Specifies the target revision/changeset revset expression.", IsCommon: true},
		{ID: "jj_m", ToolID: "jj", Subcommand: "describe", FlagName: "-m", FlagType: "string", Explanation: "Sets the description (commit message) directly.", IsCommon: true},
		{ID: "jj_b", ToolID: "jj", Subcommand: "new", FlagName: "-b", FlagType: "string", Explanation: "Create new commit as a bookmark branch.", IsCommon: true},
		{ID: "jj_s", ToolID: "jj", Subcommand: "squash", FlagName: "-s", FlagType: "string", Explanation: "Move changes into a specified revision instead of the parent.", IsCommon: true},

		// Docker
		{ID: "docker_d", ToolID: "docker", Subcommand: "run", FlagName: "-d", FlagType: "boolean", Explanation: "Run container in background and print container ID (detached).", IsCommon: true},
		{ID: "docker_rm", ToolID: "docker", Subcommand: "run", FlagName: "--rm", FlagType: "boolean", Explanation: "Automatically remove the container when it exits.", IsCommon: true},
		{ID: "docker_it", ToolID: "docker", Subcommand: "run", FlagName: "-it", FlagType: "boolean", Explanation: "Allocate a pseudo-TTY connected to the container's stdin.", IsCommon: true},
		{ID: "docker_v", ToolID: "docker", Subcommand: "run", FlagName: "-v", FlagType: "string", Explanation: "Bind mount a volume from the host into the container.", IsCommon: true},
	}

	for _, f := range flags {
		_, _ = db.Exec(`
			INSERT OR IGNORE INTO flag_definitions (id, tool_id, subcommand, flag_name, flag_type, explanation, is_common)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			f.ID, f.ToolID, f.Subcommand, f.FlagName, f.FlagType, f.Explanation, f.IsCommon,
		)
	}
}
