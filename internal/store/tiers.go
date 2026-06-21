package store

// nextTier returns the next tier in the model hierarchy.
// If the model is not found or is already the top tier, it returns empty string with ok=false.
func (s *sqliteStore) nextTier(model string) (string, bool) {
	for i, m := range s.escalationLadder {
		if m == model {
			// If this is the last element, we're at the top tier
			if i == len(s.escalationLadder)-1 {
				return "", false
			}
			// Return the next tier
			return s.escalationLadder[i+1], true
		}
	}
	// Model not found in the ladder
	return "", false
}

// isTopTier checks if the given model is the top tier.
func (s *sqliteStore) isTopTier(model string) bool {
	if len(s.escalationLadder) == 0 {
		return false
	}
	return s.escalationLadder[len(s.escalationLadder)-1] == model
}
