package engine

import (
	"math"
	"time"

	"learning-mcp/internal/db"
)

// SM2Result holds the computed next repetition state
type SM2Result struct {
	Repetitions  int
	IntervalDays float64
	EaseFactor   float64
	NextReviewAt time.Time
	MasteryScore float64
	Status       string
}

// CalculateSM2 calculates the next review schedule using the SM-2 algorithm
// qualityScore: 0 (blackout) to 5 (perfect instant recall)
func CalculateSM2(current *db.RepetitionItem, qualityScore int, now time.Time) SM2Result {
	if qualityScore < 0 {
		qualityScore = 0
	}
	if qualityScore > 5 {
		qualityScore = 5
	}

	easeFactor := 2.5
	repetitions := 0
	intervalDays := 0.0

	if current != nil {
		if current.EaseFactor >= 1.3 {
			easeFactor = current.EaseFactor
		}
		repetitions = current.Repetitions
		intervalDays = current.IntervalDays
	}

	// Update ease factor: EF' = EF + (0.1 - (5 - q) * (0.08 + (5 - q) * 0.02))
	qDiff := float64(5 - qualityScore)
	easeFactor = easeFactor + (0.1 - qDiff*(0.08+qDiff*0.02))
	if easeFactor < 1.3 {
		easeFactor = 1.3
	}

	if qualityScore >= 3 {
		// Successful recall
		switch repetitions {
		case 0:
			intervalDays = 1.0 // 1 day for first successful review
		case 1:
			intervalDays = 3.0 // 3 days for second successful review
		default:
			intervalDays = intervalDays * easeFactor
		}
		repetitions++
	} else {
		// Failed recall / reset
		repetitions = 0
		intervalDays = 0.25 // Review again in 6 hours
	}

	// Calculate next review timestamp with sub-hour float precision
	nextReview := now.Add(time.Duration(intervalDays * 24 * float64(time.Hour)))

	// Calculate mastery score (0.0 to 1.0) based on repetitions and interval
	mastery := calculateMastery(repetitions, intervalDays)

	status := "learning"
	if mastery >= 0.85 && repetitions >= 4 {
		status = "mastered"
	} else if repetitions > 0 {
		status = "reviewing"
	}

	return SM2Result{
		Repetitions:  repetitions,
		IntervalDays: intervalDays,
		EaseFactor:   math.Round(easeFactor*100) / 100,
		NextReviewAt: nextReview,
		MasteryScore: math.Round(mastery*100) / 100,
		Status:       status,
	}
}

// calculateMastery produces a normalized 0.0 - 1.0 mastery score
func calculateMastery(reps int, intervalDays float64) float64 {
	if reps == 0 {
		return 0.1
	}

	// Sigmoid curve on interval and repetitions
	repFactor := math.Min(1.0, float64(reps)/5.0) * 0.5
	intervalFactor := math.Min(1.0, intervalDays/21.0) * 0.5

	score := repFactor + intervalFactor
	if score > 1.0 {
		score = 1.0
	}
	return score
}
