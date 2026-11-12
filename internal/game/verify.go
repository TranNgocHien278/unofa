package game

import (
	"time"

	"github.com/unofa/unofa/internal/errs"
)

// decisionKind names how an awaiting_challenge state settles.
type decisionKind uint8

const (
	decisionAccept decisionKind = iota
	decisionTimeout
	decisionChallenge
)

// Verify runs the 10-step check of 02 §Thứ tự verify and only mutates state once
// every check has passed. A failure returns one engine code and broadcasts nothing.
func Verify(s *State, a Action, userID int64, now time.Time) ([]Event, error) {
	// 1. Session validity is the transport's job; userID arrives resolved.

	// 2. Player of this match, seat still alive.
	idx := s.findPlayerIdx(userID)
	if idx < 0 {
		return nil, errNotInMatch
	}
	if s.players[idx].Status == Forfeit {
		return nil, errNotInMatch
	}

	if s.phase == Ended {
		return nil, errMatchEnded
	}

	// 3. Phase vs action.
	switch a.Action {
	case "challenge", "accept":
		if s.phase != AwaitingChallenge {
			return nil, errNotYourTurn
		}
		if s.pendingChallenger == nil || *s.pendingChallenger != userID {
			return nil, errNotYourTurn
		}
		// 4. Deadline check happens after the phase check so a player who is not
		// the challenger never learns the state expired.
		if !now.Before(s.turnDeadline) {
			return nil, errTurnExpired
		}
		kind := decisionAccept
		if a.Action == "challenge" {
			kind = decisionChallenge
		}
		return s.resolveChallenge(kind, now)
	case "play", "draw", "pass":
		if s.phase == AwaitingChallenge {
			// The challenger must accept or challenge first.
			if s.pendingChallenger != nil && *s.pendingChallenger == userID {
				return nil, errChallengePending
			}
			return nil, errNotYourTurn
		}
	case "call_uno", "catch_uno":
		// These are legal outside the owner's turn (catching in particular).
	default:
		return nil, errInvalidMove
	}

	// 4/5. Turn ownership and deadline apply to play/draw/pass only.
	switch a.Action {
	case "play", "draw", "pass":
		if s.phase != Playing || s.current != idx {
			return nil, errNotYourTurn
		}
		if !now.Before(s.turnDeadline) {
			return nil, errTurnExpired
		}
	}

	switch a.Action {
	case "play":
		s.closeUnoWindowFor(userID)
		return s.applyPlay(idx, a, now)
	case "draw":
		s.closeUnoWindowFor(userID)
		return s.applyDraw(idx, now)
	case "pass":
		return s.applyPass(idx, now)
	case "call_uno":
		return s.applyCallUno(idx)
	case "catch_uno":
		return s.applyCatchUno(userID)
	}
	return nil, errInvalidMove
}

// applyPlay performs a card play, running steps 6-9 of the verify order.
func (s *State) applyPlay(idx int, a Action, now time.Time) ([]Event, error) {
	if a.Card == nil {
		return nil, errInvalidMove
	}
	card, err := FromWire(*a.Card)
	if err != nil {
		return nil, errInvalidMove
	}
	p := s.players[idx]

	// 6. The card must be in the server's copy of the hand.
	if !handHas(p.Hand, card) {
		return nil, errCardNotInHand
	}

	// 7a. Wild cards need a chosen color that is not NONE.
	var chosen Color
	if card.Rank == Wild || card.Rank == Wild4 {
		c, ok := ParseColor(a.ChosenColor)
		if !ok || c == None {
			return nil, errMissingColor
		}
		chosen = c
	}

	// 7b. The card must be legal on the current top card.
	if !playable(card, s.topDiscard(), s.activeColor) {
		return nil, errInvalidMove
	}

	// 8. After drawing, only the drawn card may be played.
	if s.drawnThisTurn != nil && *s.drawnThisTurn != card {
		return nil, errMustPlayDrawn
	}

	// 9. WILD4 snapshots whether the hand matched the color before the play.
	hadMatch := card.Rank == Wild4 && handHasColor(p.Hand, s.activeColor)

	// 10. Mutate.
	newHand, _ := removeFromHand(p.Hand, card)
	p.Hand = newHand
	s.discard = append(s.discard, card)
	s.drawnThisTurn = nil
	if card.Rank == Wild || card.Rank == Wild4 {
		s.activeColor = chosen
	} else {
		s.activeColor = card.Color
	}

	evts := []Event{s.evt("action", map[string]any{
		"match_id":      s.id,
		"player_id":     p.UserID,
		"action":        "play",
		"card":          ToWire(card),
		"chosen_color":  chosenString(card, chosen),
		"result":        "success",
		"at":            now.Format(time.RFC3339),
	})}

	// UNO window: a player who drops to one card without calling opens the window
	// for everyone else until the next player acts (D13).
	s.unoWindowVictim = nil
	if len(p.Hand) == 1 && !p.UnoCalled {
		victim := p.UserID
		s.unoWindowVictim = &victim
	}

	// Confirmed win when the hand is empty, except for WILD4 which stays pending.
	if len(p.Hand) == 0 {
		if card.Rank == Wild4 {
			nextIdx := s.nextLiveFrom(idx, 1)
			challenger := s.players[nextIdx].UserID
			s.phase = AwaitingChallenge
			s.pendingChallenger = &challenger
			wild4By := p.UserID
			s.wild4Player = &wild4By
			s.hadMatchingColor = hadMatch
			s.turnDeadline = now.Add(s.cfg.TurnTimeout)
			return evts, nil
		}
		s.setWinner(p.UserID)
		evts = append(evts, s.matchEnd(p.UserID, "out_of_cards"))
		return evts, nil
	}

	evts = append(evts, s.applyEffect(card, idx, now, hadMatch)...)
	return evts, nil
}

// applyEffect advances the turn according to the played card (02 §play step 6-9)
// and returns any extra events (the DRAW2 victim's private cards).
func (s *State) applyEffect(card Card, idx int, now time.Time, hadMatch bool) []Event {
	switch card.Rank {
	case Skip:
		s.advanceCurrentBy(2)
	case Reverse:
		if s.liveCount() == 2 {
			// With two players REVERSE acts as SKIP: the player goes again.
			s.turnDeadline = now.Add(s.cfg.TurnTimeout)
			return nil
		}
		s.direction = -s.direction
		s.advanceCurrentBy(1)
	case Draw2:
		victimIdx := s.nextLiveFrom(idx, 1)
		drawn := s.drawNFor(victimIdx, 2)
		s.advanceCurrentBy(2)
		s.turnDeadline = now.Add(s.cfg.TurnTimeout)
		if len(drawn) == 0 {
			return nil
		}
		victim := s.players[victimIdx].UserID
		return []Event{s.evtPrivate("action", map[string]any{
			"match_id":         s.id,
			"player_id":        victim,
			"action":           "draw2_penalty",
			"target_player_id": victim,
			"result":           "success",
			"at":               now.Format(time.RFC3339),
		}, map[int64]any{victim: map[string]any{"cards": ToWireAll(drawn)}})}
	case Wild4:
		nextIdx := s.nextLiveFrom(idx, 1)
		challenger := s.players[nextIdx].UserID
		s.phase = AwaitingChallenge
		s.pendingChallenger = &challenger
		by := s.players[idx].UserID
		s.wild4Player = &by
		s.hadMatchingColor = hadMatch
		s.turnDeadline = now.Add(s.cfg.TurnTimeout)
		return nil
	default:
		s.advanceCurrentBy(1)
	}
	s.turnDeadline = now.Add(s.cfg.TurnTimeout)
	return nil
}

// resolveChallenge settles an awaiting_challenge state.
func (s *State) resolveChallenge(kind decisionKind, now time.Time) ([]Event, error) {
	if s.phase != AwaitingChallenge || s.pendingChallenger == nil || s.wild4Player == nil {
		return nil, errNotYourTurn
	}
	challengerID := *s.pendingChallenger
	wild4ID := *s.wild4Player
	challengerIdx := s.findPlayerIdx(challengerID)
	wild4Idx := s.findPlayerIdx(wild4ID)
	if challengerIdx < 0 || wild4Idx < 0 {
		return nil, errNotInMatch
	}

	s.phase = Playing
	s.pendingChallenger = nil
	s.wild4Player = nil
	hadMatch := s.hadMatchingColor
	s.hadMatchingColor = false
	s.unoWindowVictim = nil
	s.drawnThisTurn = nil

	challenger := s.players[challengerIdx]
	wild4Player := s.players[wild4Idx]
	_ = challenger

	// 02 §WILD4 là lá cuối cùng: with an empty hand the win stays unconfirmed only
	// until the decision lands. A correct challenge revives the match (the drawer
	// takes 4); every other outcome confirms the win and nobody draws.
	wild4WasLast := len(wild4Player.Hand) == 0
	correctChallenge := kind == decisionChallenge && hadMatch

	if wild4WasLast && !correctChallenge {
		s.setWinner(wild4ID)
		return []Event{s.matchEnd(wild4ID, "out_of_cards")}, nil
	}

	var evts []Event
	priv := map[int64]any{}

	switch {
	case correctChallenge:
		// Correct challenge: the Wild4 player draws 4 and the challenger plays on.
		drawn := s.drawNFor(wild4Idx, 4)
		if len(drawn) > 0 {
			priv[wild4ID] = map[string]any{"cards": ToWireAll(drawn)}
		}
		s.current = challengerIdx
		s.turnDeadline = now.Add(s.cfg.TurnTimeout)
		evts = append(evts, s.evtPrivate("action", map[string]any{
			"match_id":         s.id,
			"player_id":        challengerID,
			"action":           "challenge",
			"target_player_id": wild4ID,
			"result":           "success",
			"at":               now.Format(time.RFC3339),
		}, priv))
		return evts, nil

	case kind == decisionChallenge && !hadMatch:
		// Wrong challenge: the challenger draws 6 and loses the turn.
		drawn := s.drawNFor(challengerIdx, 6)
		if len(drawn) > 0 {
			priv[challengerID] = map[string]any{"cards": ToWireAll(drawn)}
		}
		evts = append(evts, s.evtPrivate("action", map[string]any{
			"match_id":         s.id,
			"player_id":        challengerID,
			"action":           "challenge",
			"target_player_id": wild4ID,
			"result":           "fail",
			"at":               now.Format(time.RFC3339),
		}, priv))
	}

	// Accept, timeout and a challenger forfeit all land here: draw 4, lose the turn.
	if kind != decisionChallenge {
		drawn := s.drawNFor(challengerIdx, 4)
		if len(drawn) > 0 {
			priv[challengerID] = map[string]any{"cards": ToWireAll(drawn)}
		}
		evts = append(evts, s.evtPrivate("action", map[string]any{
			"match_id":         s.id,
			"player_id":        challengerID,
			"action":           "accept",
			"target_player_id": wild4ID,
			"result":           "success",
			"at":               now.Format(time.RFC3339),
		}, priv))
	}

	s.advanceFromChallenger(challengerIdx)
	s.turnDeadline = now.Add(s.cfg.TurnTimeout)
	return evts, nil
}

// advanceFromChallenger moves the turn to the player after the challenger.
func (s *State) advanceFromChallenger(challengerIdx int) {
	s.current = s.nextLiveFrom(challengerIdx, 1)
}

// applyDraw draws one card for the current player, privately.
func (s *State) applyDraw(idx int, now time.Time) ([]Event, error) {
	if s.drawnThisTurn != nil {
		return nil, errAlreadyDrew
	}
	p := s.players[idx]
	drawn := s.drawNFor(idx, 1)

	var priv map[int64]any
	data := map[string]any{
		"match_id":  s.id,
		"player_id": p.UserID,
		"action":    "draw",
		"result":    "success",
		"at":        now.Format(time.RFC3339),
	}
	if len(drawn) > 0 {
		s.drawnThisTurn = &drawn[0]
		priv = map[int64]any{p.UserID: map[string]any{"cards": ToWireAll(drawn)}}
	} else {
		// Both piles empty: nothing could be drawn, but the turn must still be able
		// to end with pass (02 §draw step 1 bullet 3). An unplayable sentinel records
		// "already tried" without granting a card to play.
		empty := Card{Color: None, Rank: Wild4}
		s.drawnThisTurn = &empty
	}
	// Closed piles: the draw still ends the turn, which `pass` then handles.
	s.turnDeadline = now.Add(s.cfg.TurnTimeout)
	return []Event{s.evtPrivate("action", data, priv)}, nil
}

// applyPass ends the turn after a draw.
func (s *State) applyPass(idx int, now time.Time) ([]Event, error) {
	if s.drawnThisTurn == nil {
		return nil, errPassWithoutDraw
	}
	p := s.players[idx]
	s.drawnThisTurn = nil
	s.unoWindowVictim = nil
	s.advanceCurrentBy(1)
	s.turnDeadline = now.Add(s.cfg.TurnTimeout)
	return []Event{s.evt("action", map[string]any{
		"match_id":  s.id,
		"player_id": p.UserID,
		"action":    "pass",
		"result":    "success",
		"at":        now.Format(time.RFC3339),
	})}, nil
}

func chosenString(c Card, chosen Color) string {
	if c.Rank == Wild || c.Rank == Wild4 {
		return chosen.String()
	}
	return ""
}

var _ = errs.New
