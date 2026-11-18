package game

import "time"

// View is the per-viewer snapshot. Shape matches docs/spec/04-http-api.md §Ván and
// the `match_start` payload of docs/spec/05-realtime.md §Event.
//
// It never carries another player's hand, the deck order, or the deck array
// (05 §Phân kênh).
type View struct {
	MatchID   string      `json:"match_id"`
	Seq       int         `json:"seq"`
	ServerNow string      `json:"server_now"`
	You       YouView     `json:"you"`
	Public    PublicView  `json:"public"`
}

// YouView is the private half: only the requesting player's cards.
type YouView struct {
	UserID     int64      `json:"user_id"`
	Hand       []WireCard `json:"hand"`
	CanCallUno bool       `json:"can_call_uno"`
	CanCatchUno *int64    `json:"can_catch_uno"`
	IsYourTurn bool       `json:"is_your_turn"`
	// DrewThisTurn tells the client to offer Pass instead of Draw (02 §draw).
	DrewThisTurn bool `json:"drew_this_turn"`
}

// PublicView is everything every player may see.
type PublicView struct {
	Players        []PublicPlayer `json:"players"`
	TopCard        WireCard       `json:"top_card"`
	ActiveColor    string         `json:"active_color"`
	DrawPileCount  int            `json:"draw_pile_count"`
	CurrentPlayer  int64          `json:"current_player"`
	Direction      int            `json:"direction"`
	TurnDeadline   string         `json:"turn_deadline"`
	Pot            int64          `json:"pot"`
	Phase          string         `json:"phase"`
	PendingChallenger *int64      `json:"pending_challenger,omitempty"`
	WinnerID       *int64         `json:"winner_id,omitempty"`
}

// PublicPlayer exposes hand counts, never cards.
type PublicPlayer struct {
	UserID    int64  `json:"user_id"`
	Seat      int    `json:"seat"`
	HandCount int    `json:"hand_count"`
	Status    string `json:"status"`
	UnoCalled bool   `json:"uno_called"`
}

// View builds the snapshot for one viewer.
func (s *State) View(userID int64, now time.Time) View {
	idx := s.findPlayerIdx(userID)

	you := YouView{UserID: userID, Hand: []WireCard{}, CanCallUno: false}
	if idx >= 0 {
		p := s.players[idx]
		you.Hand = ToWireAll(p.Hand)
		you.CanCallUno = s.canCallUno(userID)
		you.IsYourTurn = s.phase == Playing && s.current == idx
		you.DrewThisTurn = you.IsYourTurn && s.drawnThisTurn != nil
	}
	you.CanCatchUno = s.canCatchUno(userID)

	players := make([]PublicPlayer, 0, len(s.players))
	for _, p := range s.players {
		players = append(players, PublicPlayer{
			UserID:    p.UserID,
			Seat:      p.Seat,
			HandCount: len(p.Hand),
			Status:    p.Status.String(),
			UnoCalled: p.UnoCalled,
		})
	}

	var currentID int64
	if s.current >= 0 && s.current < len(s.players) {
		currentID = s.players[s.current].UserID
	}

	pub := PublicView{
		Players:       players,
		TopCard:       ToWire(s.topDiscard()),
		ActiveColor:   s.activeColor.String(),
		DrawPileCount: len(s.deck),
		CurrentPlayer: currentID,
		Direction:     s.direction,
		TurnDeadline:  s.turnDeadline.UTC().Format(time.RFC3339),
		Pot:           s.pot,
		Phase:         s.phase.String(),
	}
	if s.phase == AwaitingChallenge {
		pub.PendingChallenger = s.pendingChallenger
	}
	if s.winner != nil {
		w := *s.winner
		pub.WinnerID = &w
	}

	return View{
		MatchID:   s.id,
		Seq:       s.seq,
		ServerNow: now.UTC().Format(time.RFC3339),
		You:       you,
		Public:    pub,
	}
}

// MatchStartView is the first snapshot, sent per player after the deal and the bet.
func (s *State) MatchStartView(userID int64, now time.Time) View { return s.View(userID, now) }
