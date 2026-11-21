package game

// playable reports whether c may be played onto top with the match's active color
// (02 §Luật khớp lá). WILD4 has no "no matching color" precondition here: the paper
// rule allows a wrong play that the next player may challenge.
func playable(c Card, top Card, activeColor Color) bool {
	if c.Rank == Wild || c.Rank == Wild4 {
		return true
	}
	if c.Color == activeColor {
		return true
	}
	if c.Rank == top.Rank {
		switch {
		case c.Rank <= R9:
			return true
		case top.Rank == Skip || top.Rank == Reverse || top.Rank == Draw2:
			return true
		}
	}
	return false
}

// handHasColor reports whether the hand holds any card of the given color.
// Wild cards have color NONE and never match.
func handHasColor(hand []Card, color Color) bool {
	if color == None {
		return false
	}
	for _, c := range hand {
		if c.Color == color {
			return true
		}
	}
	return false
}
