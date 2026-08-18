package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"learning-mcp/internal/db"
)

// Safety classifications
const (
	SafetyReadOnly    = "read_only"
	SafetyMutating    = "mutating"
	SafetyDestructive = "destructive"
	SafetyPrivileged  = "privileged"
)

// ParsedFlag represents an extracted flag and its contextual meaning
type ParsedFlag struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	IsBool      bool   `json:"is_bool"`
	Explanation string `json:"explanation"`
}

// ParsedCommand represents structured command breakdown
type ParsedCommand struct {
	RawCommand      string       `json:"raw_command"`
	BaseTool        string       `json:"base_tool"`
	Subcommands     []string     `json:"subcommands"`
	Flags           []ParsedFlag `json:"flags"`
	PositionalArgs  []string     `json:"positional_args"`
	PatternTemplate string       `json:"pattern_template"`
	PatternID       string       `json:"pattern_id"`
	Summary         string       `json:"summary"`
	SafetyLevel     string       `json:"safety_level"`
	PipedCommands   []string     `json:"piped_commands,omitempty"`
}

// Tokenize splits command line honoring single/double quotes and escapes according to POSIX conventions
func Tokenize(cmd string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := rune(0)
	isEscaped := false
	hasToken := false

	for _, r := range cmd {
		if inQuote == '\'' {
			// POSIX single quotes: strictly literal, no backslash escapes
			if r == '\'' {
				inQuote = 0
				hasToken = true
			} else {
				current.WriteRune(r)
			}
			continue
		}

		if isEscaped {
			current.WriteRune(r)
			isEscaped = false
			hasToken = true
			continue
		}

		if r == '\\' {
			isEscaped = true
			continue
		}

		if inQuote == '"' {
			if r == '"' {
				inQuote = 0
				hasToken = true
			} else {
				current.WriteRune(r)
			}
			continue
		}

		if r == '\'' || r == '"' {
			inQuote = r
			continue
		}

		if unicode.IsSpace(r) {
			if current.Len() > 0 || hasToken {
				tokens = append(tokens, current.String())
				current.Reset()
				hasToken = false
			}
			continue
		}

		current.WriteRune(r)
	}

	if isEscaped {
		current.WriteRune('\\')
	}

	if current.Len() > 0 || hasToken {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// splitPipeline splits a command string by pipes outside of quotes
func splitPipeline(cmd string) []string {
	var parts []string
	var current strings.Builder
	inQuote := rune(0)
	isEscaped := false

	for _, r := range cmd {
		if inQuote == '\'' {
			current.WriteRune(r)
			if r == '\'' {
				inQuote = 0
			}
			continue
		}

		if isEscaped {
			current.WriteRune(r)
			isEscaped = false
			continue
		}

		if r == '\\' {
			isEscaped = true
			current.WriteRune(r)
			continue
		}

		if inQuote == '"' {
			current.WriteRune(r)
			if r == '"' {
				inQuote = 0
			}
			continue
		}

		if r == '\'' || r == '"' {
			inQuote = r
			current.WriteRune(r)
			continue
		}

		if r == '|' && inQuote == 0 {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}

// Parse analyzes a raw shell command line
func Parse(rawCmd string, store *db.Store) (*ParsedCommand, error) {
	trimmed := strings.TrimSpace(rawCmd)
	if trimmed == "" {
		return nil, fmt.Errorf("empty command")
	}

	pipeParts := splitPipeline(trimmed)
	primaryCmdStr := pipeParts[0]

	tokens := Tokenize(primaryCmdStr)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no tokens in command")
	}

	baseTool := tokens[0]
	if idx := strings.LastIndex(baseTool, "/"); idx != -1 {
		baseTool = baseTool[idx+1:]
	}

	var subcommands []string
	var flags []ParsedFlag
	var positionalArgs []string

	endOfOptions := false
	i := 1

	for i < len(tokens) {
		tok := tokens[i]

		if endOfOptions {
			positionalArgs = append(positionalArgs, tok)
			i++
			continue
		}

		if tok == "--" {
			endOfOptions = true
			i++
			continue
		}

		// Single dash is a positional argument (e.g. "git checkout -", "kubectl apply -f -")
		if tok == "-" {
			positionalArgs = append(positionalArgs, tok)
			i++
			continue
		}

		if strings.HasPrefix(tok, "-") {
			var flagName, flagVal string
			var isBool bool

			if strings.Contains(tok, "=") {
				eqIdx := strings.Index(tok, "=")
				flagName = tok[:eqIdx]
				flagVal = tok[eqIdx+1:]
			} else {
				flagName = tok
				subcmdStr := strings.Join(subcommands, " ")
				if isStandaloneBoolFlag(baseTool, subcmdStr, flagName) {
					isBool = true
				} else if i+1 < len(tokens) && (tokens[i+1] == "-" || !strings.HasPrefix(tokens[i+1], "-")) {
					flagVal = tokens[i+1]
					i++
				} else {
					isBool = true
				}
			}

			explanation := ""
			if store != nil {
				subcmdStr := strings.Join(subcommands, " ")
				flagDef, _ := store.FindFlag(baseTool, subcmdStr, flagName)
				if flagDef != nil {
					explanation = flagDef.Explanation
				}
			}
			if explanation == "" {
				explanation = inferFlagExplanation(baseTool, flagName, flagVal)
			}

			flags = append(flags, ParsedFlag{
				Name:        flagName,
				Value:       flagVal,
				IsBool:      isBool,
				Explanation: explanation,
			})
		} else {
			if len(flags) == 0 && len(subcommands) < 2 && isLikelySubcommand(baseTool, tok) {
				subcommands = append(subcommands, tok)
			} else {
				positionalArgs = append(positionalArgs, tok)
			}
		}
		i++
	}

	parsed := &ParsedCommand{
		RawCommand:     trimmed,
		BaseTool:       baseTool,
		Subcommands:    subcommands,
		Flags:          flags,
		PositionalArgs: positionalArgs,
	}

	if len(pipeParts) > 1 {
		parsed.PipedCommands = pipeParts[1:]
	}

	parsed.PatternTemplate = generateTemplate(parsed)
	parsed.PatternID = generatePatternID(parsed)
	parsed.SafetyLevel = classifySafety(parsed)
	parsed.Summary = generateSummary(parsed)

	return parsed, nil
}

func isStandaloneBoolFlag(tool, subcommand, flag string) bool {
	switch tool {
	case "kubectl":
		if flag == "-f" || flag == "--follow" {
			return strings.Contains(subcommand, "logs")
		}
		bools := map[string]bool{
			"-A": true, "--all-namespaces": true,
			"-w": true, "--watch": true,
			"-h": true, "--help": true,
			"-v": true, "--version": true,
		}
		return bools[flag]

	case "git":
		bools := map[string]bool{
			"-p": true, "--patch": true,
			"-a": true, "--all": true,
			"-f": true, "--force": true,
			"--force-with-lease": true,
			"--amend":            true,
			"--no-edit":          true,
			"-i":                 true,
			"-s":                 true,
			"-v":                 true,
			"-q":                 true,
			"-h":                 true,
			"--help":             true,
			"-D":                 true,
		}
		return bools[flag]

	case "docker":
		bools := map[string]bool{
			"-d": true, "--detach": true,
			"--rm":   true,
			"-it":    true,
			"-i":     true,
			"-t":     true,
			"-h":     true,
			"--help": true,
		}
		return bools[flag]

	default:
		bools := map[string]bool{
			"-h": true, "--help": true,
			"-v": true, "--version": true,
			"-y": true, "--yes": true,
			"-f": true, "--force": true,
		}
		return bools[flag]
	}
}

func isLikelySubcommand(tool, token string) bool {
	switch tool {
	case "kubectl":
		subcmds := map[string]bool{
			"get": true, "describe": true, "logs": true, "apply": true,
			"delete": true, "exec": true, "edit": true, "scale": true,
			"rollout": true, "port-forward": true, "top": true, "config": true,
			"create": true, "cp": true, "explain": true, "drain": true, "cordon": true,
			"run": true, "pods": true, "services": true, "deployments": true, "nodes": true,
		}
		return subcmds[token]
	case "git":
		subcmds := map[string]bool{
			"add": true, "commit": true, "status": true, "push": true, "pull": true,
			"fetch": true, "checkout": true, "branch": true, "rebase": true, "merge": true,
			"diff": true, "log": true, "reset": true, "stash": true, "clone": true,
			"tag": true, "cherry-pick": true, "remote": true, "switch": true, "restore": true,
			"clean": true,
		}
		return subcmds[token]
	case "jj":
		subcmds := map[string]bool{
			"new": true, "commit": true, "describe": true, "diff": true, "log": true,
			"status": true, "squash": true, "rebase": true, "abandon": true, "edit": true,
			"bookmark": true, "branch": true, "git": true, "split": true, "undo": true,
			"next": true, "prev": true, "restore": true,
		}
		return subcmds[token]
	case "docker":
		subcmds := map[string]bool{
			"run": true, "ps": true, "stop": true, "start": true, "build": true,
			"exec": true, "logs": true, "images": true, "rm": true, "rmi": true,
			"compose": true, "network": true, "volume": true, "pull": true, "push": true,
			"system": true, "down": true, "up": true,
		}
		return subcmds[token]
	case "terraform":
		subcmds := map[string]bool{
			"init": true, "plan": true, "apply": true, "destroy": true, "fmt": true,
			"validate": true, "output": true, "show": true, "state": true, "import": true,
			"rm": true,
		}
		return subcmds[token]
	default:
		return false
	}
}

func inferFlagExplanation(tool, flagName, flagVal string) string {
	switch flagName {
	case "-n", "--namespace":
		return fmt.Sprintf("Specifies target namespace '%s'.", flagVal)
	case "-l", "--selector":
		return fmt.Sprintf("Filters resources matching selector '%s'.", flagVal)
	case "-o", "--output":
		return fmt.Sprintf("Formats output as '%s'.", flagVal)
	case "-p", "--patch":
		return "Interactively select hunks to stage."
	case "-m", "--message":
		return fmt.Sprintf("Sets description/message '%s'.", flagVal)
	case "-r", "--revision":
		return fmt.Sprintf("Targets revision '%s'.", flagVal)
	case "-d", "--detach":
		return "Runs task/container in background."
	case "--rm":
		return "Cleans up / removes container upon exit."
	case "-f":
		if tool == "kubectl" && flagVal == "" {
			return "Follows / streams live log output."
		}
		return fmt.Sprintf("Specifies file or manifest '%s'.", flagVal)
	case "--file":
		return fmt.Sprintf("Specifies file or manifest '%s'.", flagVal)
	case "--follow":
		return "Streams / follows live output stream."
	case "-i", "--interactive":
		return "Enables interactive mode."
	case "-t", "--tty":
		return "Allocates a terminal TTY."
	default:
		return fmt.Sprintf("Option flag '%s'.", flagName)
	}
}

func generateTemplate(p *ParsedCommand) string {
	var parts []string
	parts = append(parts, p.BaseTool)
	parts = append(parts, p.Subcommands...)

	var flagStrs []string
	for _, f := range p.Flags {
		if f.IsBool {
			flagStrs = append(flagStrs, f.Name)
		} else {
			placeholder := placeholderForFlag(f.Name)
			flagStrs = append(flagStrs, fmt.Sprintf("%s %s", f.Name, placeholder))
		}
	}
	sort.Strings(flagStrs)
	parts = append(parts, flagStrs...)

	for _, arg := range p.PositionalArgs {
		parts = append(parts, placeholderForArg(p.BaseTool, arg))
	}

	return strings.Join(parts, " ")
}

func placeholderForFlag(flagName string) string {
	switch flagName {
	case "-n", "--namespace":
		return "<NAMESPACE>"
	case "-l", "--selector":
		return "<SELECTOR>"
	case "-o", "--output":
		return "<FORMAT>"
	case "-m", "--message":
		return "<MESSAGE>"
	case "-r", "--revision":
		return "<REVISION>"
	case "-f", "--file":
		return "<FILE>"
	case "-p", "--port":
		return "<PORT>"
	case "-v", "--volume":
		return "<VOLUME>"
	default:
		return "<VAL>"
	}
}

func placeholderForArg(tool, arg string) string {
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		return "<URL>"
	}
	if strings.Contains(arg, "/") || strings.Contains(arg, ".") {
		return "<PATH>"
	}
	return "<ARG>"
}

func generatePatternID(p *ParsedCommand) string {
	var flagNames []string
	for _, f := range p.Flags {
		flagNames = append(flagNames, f.Name)
	}
	sort.Strings(flagNames)

	key := fmt.Sprintf("%s:%s:%s", p.BaseTool, strings.Join(p.Subcommands, "_"), strings.Join(flagNames, "_"))
	hash := sha256.Sum256([]byte(key))
	slug := strings.ReplaceAll(fmt.Sprintf("%s_%s_%s", p.BaseTool, strings.Join(p.Subcommands, "_"), hex.EncodeToString(hash[:4])), "__", "_")
	slug = strings.Trim(slug, "_")
	return slug
}

func classifySafety(p *ParsedCommand) string {
	full := strings.ToLower(p.RawCommand)
	if strings.HasPrefix(full, "sudo ") {
		return SafetyPrivileged
	}

	hasFlag := func(name string) bool {
		for _, f := range p.Flags {
			if f.Name == name {
				return true
			}
			if strings.HasPrefix(f.Name, "-") && !strings.HasPrefix(f.Name, "--") && len(name) == 2 && strings.HasPrefix(name, "-") {
				if strings.ContainsRune(f.Name[1:], rune(name[1])) {
					return true
				}
			}
		}
		return false
	}

	sub := strings.Join(p.Subcommands, " ")

	switch p.BaseTool {
	case "kubectl":
		if strings.Contains(sub, "delete") || strings.Contains(sub, "drain") || strings.Contains(sub, "cordon") {
			return SafetyDestructive
		}
		if strings.Contains(sub, "apply") || strings.Contains(sub, "create") || strings.Contains(sub, "edit") ||
			strings.Contains(sub, "scale") || strings.Contains(sub, "patch") || strings.Contains(sub, "rollout") ||
			strings.Contains(sub, "run") {
			return SafetyMutating
		}
		if strings.Contains(sub, "exec") || strings.Contains(sub, "port-forward") {
			return SafetyPrivileged
		}
		return SafetyReadOnly

	case "git":
		if strings.Contains(sub, "push") {
			if hasFlag("--force") || hasFlag("-f") {
				return SafetyDestructive
			}
			return SafetyMutating
		}
		if strings.Contains(sub, "reset") && hasFlag("--hard") {
			return SafetyDestructive
		}
		if strings.Contains(sub, "clean") && (hasFlag("-f") || hasFlag("--force")) {
			return SafetyDestructive
		}
		if strings.Contains(sub, "branch") && (hasFlag("-D") || hasFlag("-d") || hasFlag("--delete")) {
			return SafetyDestructive
		}
		if strings.Contains(sub, "commit") || strings.Contains(sub, "add") || strings.Contains(sub, "merge") ||
			strings.Contains(sub, "rebase") || strings.Contains(sub, "cherry-pick") || strings.Contains(sub, "checkout") ||
			strings.Contains(sub, "switch") || strings.Contains(sub, "restore") {
			return SafetyMutating
		}
		return SafetyReadOnly

	case "jj":
		if strings.Contains(sub, "abandon") {
			return SafetyDestructive
		}
		if strings.Contains(sub, "new") || strings.Contains(sub, "commit") || strings.Contains(sub, "squash") ||
			strings.Contains(sub, "rebase") || strings.Contains(sub, "describe") || strings.Contains(sub, "split") ||
			strings.Contains(sub, "restore") {
			return SafetyMutating
		}
		return SafetyReadOnly

	case "docker":
		if strings.Contains(sub, "rm") || strings.Contains(sub, "rmi") || strings.Contains(sub, "system prune") ||
			strings.Contains(sub, "compose down") {
			return SafetyDestructive
		}
		if strings.Contains(sub, "run") || strings.Contains(sub, "build") || strings.Contains(sub, "start") ||
			strings.Contains(sub, "stop") || strings.Contains(sub, "compose up") {
			return SafetyMutating
		}
		if strings.Contains(sub, "exec") {
			return SafetyPrivileged
		}
		return SafetyReadOnly

	case "terraform":
		if strings.Contains(sub, "destroy") || strings.Contains(sub, "state rm") {
			return SafetyDestructive
		}
		if strings.Contains(sub, "apply") || strings.Contains(sub, "import") {
			return SafetyMutating
		}
		return SafetyReadOnly
	}

	if strings.Contains(full, "rm -rf") || strings.Contains(full, "drop database") || strings.Contains(full, "kill -9") {
		return SafetyDestructive
	}

	return SafetyReadOnly
}

func generateSummary(p *ParsedCommand) string {
	sub := strings.Join(p.Subcommands, " ")
	switch p.BaseTool {
	case "kubectl":
		switch {
		case strings.Contains(sub, "get pods"):
			return "Lists Kubernetes pods matching the specified namespace and filters."
		case strings.Contains(sub, "get"):
			return fmt.Sprintf("Lists Kubernetes '%s' resources.", strings.TrimPrefix(sub, "get "))
		case strings.Contains(sub, "describe"):
			return "Describes detailed state and events of the target Kubernetes resource."
		case strings.Contains(sub, "logs"):
			return "Retrieves and displays log output for a container or pod."
		case strings.Contains(sub, "apply"):
			return "Applies configuration changes to Kubernetes cluster from a file or stdin."
		case strings.Contains(sub, "delete"):
			return "Deletes Kubernetes resources from cluster."
		}
	case "git":
		switch {
		case strings.Contains(sub, "status"):
			return "Shows working tree status and staging index."
		case strings.Contains(sub, "commit"):
			return "Records staged snapshot changes into repository history."
		case strings.Contains(sub, "add"):
			return "Adds file contents to the staging index for next commit."
		case strings.Contains(sub, "push"):
			return "Updates remote refs along with associated objects."
		case strings.Contains(sub, "rebase"):
			return "Reapplies commits on top of another base tip."
		case strings.Contains(sub, "diff"):
			return "Shows changes between commits, commit and working tree, etc."
		}
	case "jj":
		switch {
		case strings.Contains(sub, "status"):
			return "Displays working copy changeset status and modified files."
		case strings.Contains(sub, "new"):
			return "Creates a new working-copy commit on top of specified parent."
		case strings.Contains(sub, "describe"):
			return "Updates commit description message for target revision."
		case strings.Contains(sub, "squash"):
			return "Squashes changes from current revision into its parent."
		case strings.Contains(sub, "log"):
			return "Shows graph log of repository revisions and bookmarks."
		}
	case "docker":
		switch {
		case strings.Contains(sub, "run"):
			return "Creates and runs a new container from an image."
		case strings.Contains(sub, "ps"):
			return "Lists running or all Docker containers."
		case strings.Contains(sub, "logs"):
			return "Fetches logs of a container."
		}
	}

	if len(p.Subcommands) > 0 {
		return fmt.Sprintf("Executes '%s %s' with %d flags.", p.BaseTool, sub, len(p.Flags))
	}
	return fmt.Sprintf("Executes command '%s'.", p.BaseTool)
}
