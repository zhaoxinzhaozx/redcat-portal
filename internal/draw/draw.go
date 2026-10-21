package draw

// Participant is someone who actually joined an activity.
type Participant struct {
	UserID int64
	Email  string
}

// ChooseWinners keeps admin-selected participants first, in the given order,
// then fills any remaining slots at random from people who actually joined.
func ChooseWinners(count int, selectedIDs []int64, participants []Participant, pick func(n int) int) []Participant {
	if count < 1 {
		count = 1
	}
	byID := make(map[int64]Participant, len(participants))
	for _, participant := range participants {
		byID[participant.UserID] = participant
	}
	chosen := make([]Participant, 0, count)
	seen := map[int64]bool{}
	for _, id := range selectedIDs {
		if len(chosen) >= count {
			break
		}
		participant, ok := byID[id]
		if !ok || seen[id] {
			continue
		}
		chosen = append(chosen, participant)
		seen[id] = true
	}
	rest := make([]Participant, 0, len(participants))
	for _, participant := range participants {
		if !seen[participant.UserID] {
			rest = append(rest, participant)
		}
	}
	for len(chosen) < count && len(rest) > 0 {
		index := pick(len(rest))
		chosen = append(chosen, rest[index])
		rest = append(rest[:index], rest[index+1:]...)
	}
	return chosen
}
