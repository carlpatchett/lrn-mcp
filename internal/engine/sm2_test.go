package engine

import (
	"testing"
	"time"

	"learning-mcp/internal/db"
)

func TestCalculateSM2(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

	t.Run("first repetition with high recall (score 5)", func(t *testing.T) {
		res := CalculateSM2(nil, 5, now)
		if res.Repetitions != 1 {
			t.Errorf("expected 1 repetition, got %d", res.Repetitions)
		}
		if res.IntervalDays != 1.0 {
			t.Errorf("expected 1.0 day interval, got %f", res.IntervalDays)
		}
		if res.EaseFactor < 2.5 {
			t.Errorf("expected ease factor >= 2.5, got %f", res.EaseFactor)
		}
		if res.Status != "reviewing" {
			t.Errorf("expected status 'reviewing', got %s", res.Status)
		}
	})

	t.Run("second repetition with good recall (score 4)", func(t *testing.T) {
		current := &db.RepetitionItem{
			Repetitions:  1,
			IntervalDays: 1.0,
			EaseFactor:   2.6,
		}
		res := CalculateSM2(current, 4, now)
		if res.Repetitions != 2 {
			t.Errorf("expected 2 repetitions, got %d", res.Repetitions)
		}
		if res.IntervalDays != 3.0 {
			t.Errorf("expected 3.0 days interval, got %f", res.IntervalDays)
		}
	})

	t.Run("third repetition applies ease factor", func(t *testing.T) {
		current := &db.RepetitionItem{
			Repetitions:  2,
			IntervalDays: 3.0,
			EaseFactor:   2.5,
		}
		res := CalculateSM2(current, 4, now)
		if res.Repetitions != 3 {
			t.Errorf("expected 3 repetitions, got %d", res.Repetitions)
		}
		if res.IntervalDays <= 3.0 {
			t.Errorf("expected interval > 3.0 days, got %f", res.IntervalDays)
		}
	})

	t.Run("failed recall (score 1) resets interval and repetitions", func(t *testing.T) {
		current := &db.RepetitionItem{
			Repetitions:  5,
			IntervalDays: 20.0,
			EaseFactor:   2.5,
		}
		res := CalculateSM2(current, 1, now)
		if res.Repetitions != 0 {
			t.Errorf("expected 0 repetitions on blackout/failure, got %d", res.Repetitions)
		}
		if res.IntervalDays != 0.25 {
			t.Errorf("expected 0.25 days (6 hours) interval on failure, got %f", res.IntervalDays)
		}
		if res.Status != "learning" {
			t.Errorf("expected status 'learning' on failure, got %s", res.Status)
		}
	})

	t.Run("ease factor clamps to minimum 1.3", func(t *testing.T) {
		current := &db.RepetitionItem{
			Repetitions:  0,
			IntervalDays: 0.0,
			EaseFactor:   1.3,
		}
		res := CalculateSM2(current, 0, now)
		if res.EaseFactor < 1.3 {
			t.Errorf("expected ease factor clamped to 1.3, got %f", res.EaseFactor)
		}
	})

	t.Run("mastery threshold transition", func(t *testing.T) {
		current := &db.RepetitionItem{
			Repetitions:  4,
			IntervalDays: 25.0,
			EaseFactor:   2.6,
		}
		res := CalculateSM2(current, 5, now)
		if res.Status != "mastered" {
			t.Errorf("expected status 'mastered' after 5 strong reviews, got %s (mastery: %f)", res.Status, res.MasteryScore)
		}
	})
}
