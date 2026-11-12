// Package clock makes the 20s / 90s / 5s timers testable without sleeping
// (docs/spec/07 §Thời gian trong code).
package clock

import "time"

type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Fake is a settable clock for tests.
type Fake struct{ T time.Time }

func (f *Fake) Now() time.Time { return f.T }

func (f *Fake) Advance(d time.Duration) { f.T = f.T.Add(d) }
