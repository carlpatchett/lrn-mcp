package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// GetTool retrieves a tracked tool by ID
func (s *Store) GetTool(id string) (*Tool, error) {
	row := s.db.QueryRow("SELECT id, enabled, strictness, created_at FROM tools WHERE id = ?", id)
	t := &Tool{}
	var enabledInt int
	var createdAt string
	if err := row.Scan(&t.ID, &enabledInt, &t.Strictness, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	t.Enabled = enabledInt == 1
	t.CreatedAt = parseTimestamp(createdAt)
	return t, nil
}

// UpsertPattern inserts or updates a command pattern
func (s *Store) UpsertPattern(p *CommandPattern) error {
	_, err := s.db.Exec(`
		INSERT INTO command_patterns (id, tool_id, pattern_template, canonical_tokens, summary, safety_level)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			summary = excluded.summary,
			safety_level = excluded.safety_level
	`, p.ID, p.ToolID, p.PatternTemplate, p.CanonicalTokens, p.Summary, p.SafetyLevel)
	return err
}

// GetPattern retrieves a pattern by ID
func (s *Store) GetPattern(id string) (*CommandPattern, error) {
	row := s.db.QueryRow(`
		SELECT id, tool_id, pattern_template, canonical_tokens, summary, safety_level, created_at
		FROM command_patterns WHERE id = ?
	`, id)
	p := &CommandPattern{}
	var createdAt string
	if err := row.Scan(&p.ID, &p.ToolID, &p.PatternTemplate, &p.CanonicalTokens, &p.Summary, &p.SafetyLevel, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	p.CreatedAt = parseTimestamp(createdAt)
	return p, nil
}

// FindFlag looks up explanation for a given tool flag
func (s *Store) FindFlag(toolID, subcommand, flagName string) (*FlagDefinition, error) {
	// Try subcommand-specific flag first, then fallback to global tool flag
	query := `
		SELECT id, tool_id, subcommand, flag_name, flag_type, explanation, is_common
		FROM flag_definitions
		WHERE tool_id = ? AND (subcommand = ? OR subcommand = '') AND flag_name = ?
		ORDER BY subcommand DESC LIMIT 1
	`
	row := s.db.QueryRow(query, toolID, subcommand, flagName)
	f := &FlagDefinition{}
	var isCommonInt int
	var subcmd sql.NullString
	if err := row.Scan(&f.ID, &f.ToolID, &subcmd, &f.FlagName, &f.FlagType, &f.Explanation, &isCommonInt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	f.Subcommand = subcmd.String
	f.IsCommon = isCommonInt == 1
	return f, nil
}

// GetRepetitionItem retrieves SM-2 repetition state
func (s *Store) GetRepetitionItem(id string) (*RepetitionItem, error) {
	row := s.db.QueryRow(`
		SELECT id, item_type, target_id, repetitions, interval_days, ease_factor, last_reviewed_at, next_review_at, mastery_score, status
		FROM repetition_items WHERE id = ?
	`, id)
	item := &RepetitionItem{}
	var lastReviewed, nextReview sql.NullString
	if err := row.Scan(
		&item.ID, &item.ItemType, &item.TargetID, &item.Repetitions,
		&item.IntervalDays, &item.EaseFactor, &lastReviewed,
		&nextReview, &item.MasteryScore, &item.Status,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if lastReviewed.Valid {
		t := parseTimestamp(lastReviewed.String)
		item.LastReviewedAt = &t
	}
	if nextReview.Valid {
		item.NextReviewAt = parseTimestamp(nextReview.String)
	}
	return item, nil
}

// UpsertRepetitionItem inserts or updates SM-2 repetition item
func (s *Store) UpsertRepetitionItem(item *RepetitionItem) error {
	var lastReviewedVal interface{}
	if item.LastReviewedAt != nil {
		lastReviewedVal = item.LastReviewedAt.Format(time.RFC3339)
	}

	_, err := s.db.Exec(`
		INSERT INTO repetition_items (
			id, item_type, target_id, repetitions, interval_days, ease_factor, last_reviewed_at, next_review_at, mastery_score, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			repetitions = excluded.repetitions,
			interval_days = excluded.interval_days,
			ease_factor = excluded.ease_factor,
			last_reviewed_at = excluded.last_reviewed_at,
			next_review_at = excluded.next_review_at,
			mastery_score = excluded.mastery_score,
			status = excluded.status
	`, item.ID, item.ItemType, item.TargetID, item.Repetitions, item.IntervalDays, item.EaseFactor,
		lastReviewedVal, item.NextReviewAt.Format(time.RFC3339), item.MasteryScore, item.Status)
	return err
}

// DueItemWithPattern combines repetition item with its command pattern info
type DueItemWithPattern struct {
	Item    RepetitionItem `json:"item"`
	Pattern CommandPattern `json:"pattern"`
}

// GetDueRepetitionItems retrieves due reviews
func (s *Store) GetDueRepetitionItems(limit int, toolFilter string) ([]DueItemWithPattern, error) {
	if limit <= 0 {
		limit = 10
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var rows *sql.Rows
	var err error

	if toolFilter != "" {
		query := `
			SELECT r.id, r.item_type, r.target_id, r.repetitions, r.interval_days, r.ease_factor,
			       r.last_reviewed_at, r.next_review_at, r.mastery_score, r.status,
			       p.id, p.tool_id, p.pattern_template, p.canonical_tokens, p.summary, p.safety_level
			FROM repetition_items r
			JOIN command_patterns p ON r.target_id = p.id
			WHERE r.status != 'ignored' AND r.next_review_at <= ? AND p.tool_id = ?
			ORDER BY r.next_review_at ASC
			LIMIT ?
		`
		rows, err = s.db.Query(query, now, toolFilter, limit)
	} else {
		query := `
			SELECT r.id, r.item_type, r.target_id, r.repetitions, r.interval_days, r.ease_factor,
			       r.last_reviewed_at, r.next_review_at, r.mastery_score, r.status,
			       p.id, p.tool_id, p.pattern_template, p.canonical_tokens, p.summary, p.safety_level
			FROM repetition_items r
			JOIN command_patterns p ON r.target_id = p.id
			WHERE r.status != 'ignored' AND r.next_review_at <= ?
			ORDER BY r.next_review_at ASC
			LIMIT ?
		`
		rows, err = s.db.Query(query, now, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DueItemWithPattern
	for rows.Next() {
		var d DueItemWithPattern
		var lastReviewed, nextReview sql.NullString
		if err := rows.Scan(
			&d.Item.ID, &d.Item.ItemType, &d.Item.TargetID, &d.Item.Repetitions, &d.Item.IntervalDays,
			&d.Item.EaseFactor, &lastReviewed, &nextReview, &d.Item.MasteryScore, &d.Item.Status,
			&d.Pattern.ID, &d.Pattern.ToolID, &d.Pattern.PatternTemplate, &d.Pattern.CanonicalTokens,
			&d.Pattern.Summary, &d.Pattern.SafetyLevel,
		); err != nil {
			return nil, err
		}
		if lastReviewed.Valid {
			t := parseTimestamp(lastReviewed.String)
			d.Item.LastReviewedAt = &t
		}
		if nextReview.Valid {
			d.Item.NextReviewAt = parseTimestamp(nextReview.String)
		}
		results = append(results, d)
	}
	return results, nil
}

// LogExecution stores an execution event
func (s *Store) LogExecution(log *ExecutionLog) error {
	var patternVal interface{}
	if log.PatternID != "" {
		patternVal = log.PatternID
	}
	var isInteractiveInt = 0
	if log.IsInteractive {
		isInteractiveInt = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO execution_logs (
			id, session_id, raw_command, pattern_id, mode, is_interactive, action_taken, quality_score, response_time_ms, executed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.SessionID, log.RawCommand, patternVal, log.Mode, isInteractiveInt, log.ActionTaken,
		log.QualityScore, log.ResponseTimeMs, log.ExecutedAt.Format(time.RFC3339))
	return err
}

// AddSnoozeRule registers a temporary snooze
func (s *Store) AddSnoozeRule(rule *SnoozeRule) error {
	_, err := s.db.Exec(`
		INSERT INTO snooze_rules (id, scope, target_id, reason, snooze_until)
		VALUES (?, ?, ?, ?, ?)
	`, rule.ID, rule.Scope, rule.TargetID, rule.Reason, rule.SnoozeUntil.Format(time.RFC3339))
	return err
}

// IsSnoozed checks if tool, pattern, or global learning is currently snoozed
func (s *Store) IsSnoozed(toolID, patternID string) (bool, *SnoozeRule, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	query := `
		SELECT id, scope, target_id, reason, snooze_until, created_at
		FROM snooze_rules
		WHERE snooze_until > ? AND (
			scope = 'global' OR
			(scope = 'tool' AND target_id = ?) OR
			(scope = 'pattern' AND target_id = ?)
		)
		ORDER BY snooze_until DESC LIMIT 1
	`
	row := s.db.QueryRow(query, now, toolID, patternID)
	r := &SnoozeRule{}
	var snoozeUntil, createdAt string
	var targetID sql.NullString
	if err := row.Scan(&r.ID, &r.Scope, &targetID, &r.Reason, &snoozeUntil, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, err
	}
	r.TargetID = targetID.String
	r.SnoozeUntil = parseTimestamp(snoozeUntil)
	r.CreatedAt = parseTimestamp(createdAt)
	return true, r, nil
}

// AddKnownExclusion permanently excludes a pattern or command
func (s *Store) AddKnownExclusion(ex *KnownExclusion) error {
	_, err := s.db.Exec(`
		INSERT INTO known_exclusions (id, scope, target, marked_reason)
		VALUES (?, ?, ?, ?)
	`, ex.ID, ex.Scope, ex.Target, ex.MarkedReason)
	return err
}

// IsKnown checks if pattern or command is permanently excluded
func (s *Store) IsKnown(toolID, patternID, exactCommand string) (bool, *KnownExclusion, error) {
	query := `
		SELECT id, scope, target, marked_reason, created_at
		FROM known_exclusions
		WHERE (scope = 'tool' AND target = ?)
		   OR (scope = 'pattern' AND target = ?)
		   OR (scope = 'exact_command' AND target = ?)
		LIMIT 1
	`
	row := s.db.QueryRow(query, toolID, patternID, exactCommand)
	ex := &KnownExclusion{}
	var createdAt string
	if err := row.Scan(&ex.ID, &ex.Scope, &ex.Target, &ex.MarkedReason, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, err
	}
	ex.CreatedAt = parseTimestamp(createdAt)
	return true, ex, nil
}

// GetStats returns summary learning statistics
func (s *Store) GetStats() (*LearningStats, error) {
	stats := &LearningStats{
		ToolBreakdown: make(map[string]int),
	}

	now := time.Now().UTC().Format(time.RFC3339)
	query := `
		SELECT
			(SELECT COUNT(*) FROM command_patterns),
			(SELECT COUNT(*) FROM repetition_items WHERE status != 'ignored' AND next_review_at <= ?),
			(SELECT COUNT(*) FROM repetition_items WHERE status = 'mastered'),
			(SELECT COUNT(*) FROM repetition_items WHERE status = 'learning'),
			(SELECT COUNT(*) FROM execution_logs),
			(SELECT COUNT(*) FROM snooze_rules WHERE snooze_until > ?)
	`
	err := s.db.QueryRow(query, now, now).Scan(
		&stats.TotalPatternsTracked,
		&stats.DueReviewsCount,
		&stats.MasteredCount,
		&stats.LearningCount,
		&stats.TotalExecutions,
		&stats.ActiveSnoozesCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed fetching stats: %w", err)
	}

	rows, err := s.db.Query("SELECT tool_id, COUNT(*) FROM command_patterns GROUP BY tool_id")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var toolID string
			var count int
			if err := rows.Scan(&toolID, &count); err == nil {
				stats.ToolBreakdown[toolID] = count
			}
		}
	}

	return stats, nil
}

// ListSnoozes retrieves active snooze rules
func (s *Store) ListSnoozes() ([]SnoozeRule, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.Query("SELECT id, scope, target_id, reason, snooze_until, created_at FROM snooze_rules WHERE snooze_until > ? ORDER BY snooze_until ASC", now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SnoozeRule
	for rows.Next() {
		var r SnoozeRule
		var targetID sql.NullString
		var snoozeUntil, createdAt string
		if err := rows.Scan(&r.ID, &r.Scope, &targetID, &r.Reason, &snoozeUntil, &createdAt); err != nil {
			return nil, err
		}
		r.TargetID = targetID.String
		r.SnoozeUntil = parseTimestamp(snoozeUntil)
		r.CreatedAt = parseTimestamp(createdAt)
		list = append(list, r)
	}
	return list, nil
}

// ListExclusions retrieves all permanently marked exclusions
func (s *Store) ListExclusions() ([]KnownExclusion, error) {
	rows, err := s.db.Query("SELECT id, scope, target, marked_reason, created_at FROM known_exclusions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []KnownExclusion
	for rows.Next() {
		var e KnownExclusion
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Scope, &e.Target, &e.MarkedReason, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTimestamp(createdAt)
		list = append(list, e)
	}
	return list, nil
}
