package game

import "time"

// UNO rules: 02 §UNO.
//
// The flag lives per player and clears whenever the hand grows back above one card
// (§Cờ). A player who lands on one card without calling opens a catch window that
// stays open until the next player acts, the match ends, or a challenge/accept is
// decided (D13).

// canCallUno reports whether userID may press UNO right now: only with exactly
// one card left, right after playing down to it, while their catch window is
// still open. Holding two cards never qualifies, not even on their own turn.
func (s *State) canCallUno(userID int64) bool {
	idx := s.findPlayerIdx(userID)
	if idx < 0 {
		return false
	}
	p := s.players[idx]
	if p.Status == Forfeit || p.UnoCalled {
		return false
	}
	return len(p.Hand) == 1 && s.unoWindowVictim != nil && *s.unoWindowVictim == userID
}

// canCatchUno returns the victim id a viewer may catch, or nil. It is a per-viewer
// value: the victim themselves sees nil.
func (s *State) canCatchUno(viewer int64) *int64 {
	if s.unoWindowVictim == nil {
		return nil
	}
	victim := *s.unoWindowVictim
	if victim == viewer {
		return nil
	}
	idx := s.findPlayerIdx(victim)
	if idx < 0 {
		return nil
	}
	if s.players[idx].Status == Forfeit {
		return nil
	}
	return &victim
}

// applyCallUno sets the caller's flag. It is legal outside their own turn, because
// the window is open until the next player acts.
func (s *State) applyCallUno(idx int) ([]Event, error) {
	p := s.players[idx]
	if !s.canCallUno(p.UserID) {
		if p.UnoCalled {
			return nil, errUnoAlreadyCalled
		}
		return nil, errCannotCallUno
	}
	p.UnoCalled = true
	s.unoWindowVictim = nil
	return []Event{s.evt("action", map[string]any{
		"match_id":  s.id,
		"player_id": p.UserID,
		"action":    "call_uno",
		"result":    "success",
	})}, nil
}

// applyCatchUno penalizes an uncalled one-card player with two cards.
func (s *State) applyCatchUno(catcher int64) ([]Event, error) {
	if s.unoWindowVictim == nil || *s.unoWindowVictim == catcher {
		return nil, errUnoWindowClosed
	}
	victimID := *s.unoWindowVictim
	victimIdx := s.findPlayerIdx(victimID)
	if victimIdx < 0 || s.players[victimIdx].Status == Forfeit {
		return nil, errUnoWindowClosed
	}

	victim := s.players[victimIdx]
	victim.UnoCalled = true // the window is spent either way
	s.unoWindowVictim = nil

	drawn := s.drawNFor(victimIdx, 2)
	var priv map[int64]any
	if len(drawn) > 0 {
		priv = map[int64]any{victimID: map[string]any{"cards": ToWireAll(drawn)}}
	}
	return []Event{s.evtPrivate("action", map[string]any{
		"match_id":        s.id,
		"player_id":       catcher,
		"action":          "catch_uno",
		"target_player_id": victimID,
		"result":          "success",
		"at":              time.Now().UTC().Format(time.RFC3339),
	}, priv)}, nil
}

// closeUnoWindowFor is called when the next player takes their turn, which ends the
// previous window (D13).
func (s *State) closeUnoWindowFor(userID int64) {
	if s.unoWindowVictim == nil {
		return
	}
	if *s.unoWindowVictim == userID {
		return
	}
	s.unoWindowVictim = nil
}
