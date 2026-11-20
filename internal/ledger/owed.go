package ledger

import "time"

// overduePeriod is the interest period of 03 §Loan: 500 bps is 5% per 24h.
const overduePeriod = 24 * time.Hour

// roundHalfUp rounds a numerator already scaled by 10000, half up, in integers.
// Floats are banned here: one rounding drift on a repayment is money created.
func roundHalfUp(n int64) int64 { return (n + 5000) / 10000 }

// Owed returns what a loan owes at now: the in-term amount grown by one 5% period per
// full 24h past due_at, capped at capMultiplier * principal (03 §Công thức).
//
// periods is the raw floor((now - due_at)/24h) even when the cap stops the growth
// early, because the API reports how many periods the borrower is behind.
func Owed(principal int64, rateBps int, dueAt, now time.Time, overdueBps int, capMultiplier int64) (owed int64, periods int) {
	if capMultiplier <= 0 {
		capMultiplier = 2
	}
	owed = roundHalfUp(principal * int64(10000+rateBps))
	if !now.After(dueAt) {
		return owed, 0
	}
	periods = int(now.Sub(dueAt) / overduePeriod)
	capAmount := principal * capMultiplier
	bps := int64(10000 + overdueBps)
	for i := 0; i < periods && owed < capAmount; i++ {
		owed = roundHalfUp(owed * bps)
	}
	if owed > capAmount {
		owed = capAmount
	}
	return owed, periods
}

// SeizeDelay is the fixed overdue window: every loan is seized this long after due_at,
// whatever the package or term. Owed still grows 5% per 24h up to the cap meanwhile.
const SeizeDelay = 14 * 24 * time.Hour

// SeizeAt returns the instant the hourly job seizes the loan: due_at + SeizeDelay.
func SeizeAt(dueAt time.Time) time.Time { return dueAt.Add(SeizeDelay) }
