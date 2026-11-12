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

// SeizeAt returns the instant the loan reaches the cap and the hourly job seizes it.
// In-term interest is fixed, so the date is known the moment the loan is taken.
// A zero time means the loan never reaches the cap.
func SeizeAt(principal int64, rateBps int, dueAt time.Time, overdueBps int, capMultiplier int64) time.Time {
	if principal < 1 {
		return time.Time{}
	}
	if capMultiplier <= 0 {
		capMultiplier = 2
	}
	owed := roundHalfUp(principal * int64(10000+rateBps))
	capAmount := principal * capMultiplier
	if owed >= capAmount {
		return dueAt
	}
	bps := int64(10000 + overdueBps)
	// 1000 periods is 2.7 years past due; the 5% compounding cap is always reached
	// far sooner (14 periods for the smallest package).
	for n := int64(1); n <= 1000; n++ {
		owed = roundHalfUp(owed * bps)
		if owed >= capAmount {
			return dueAt.Add(time.Duration(n) * overduePeriod)
		}
	}
	return time.Time{}
}
