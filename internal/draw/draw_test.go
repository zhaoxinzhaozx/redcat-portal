package draw

import "testing"

func TestChooseWinnersUsesSelectionThenRandom(t *testing.T) {
	participants := []Participant{
		{UserID: 1, Email: "a@x.com"},
		{UserID: 2, Email: "b@x.com"},
		{UserID: 3, Email: "c@x.com"},
	}
	got := ChooseWinners(2, []int64{2}, participants, func(n int) int {
		if n != 2 {
			t.Fatalf("n=%d", n)
		}
		return 1
	})
	if len(got) != 2 || got[0].UserID != 2 || got[1].UserID != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestChooseWinnersRandomWhenNoneSelected(t *testing.T) {
	participants := []Participant{{UserID: 1, Email: "a@x.com"}, {UserID: 7, Email: "b@x.com"}}
	got := ChooseWinners(1, nil, participants, func(int) int { return 1 })
	if len(got) != 1 || got[0].UserID != 7 {
		t.Fatalf("%+v", got)
	}
}

func TestChooseWinnersStopsWhenNobodyJoined(t *testing.T) {
	if len(ChooseWinners(3, []int64{9}, nil, func(int) int { return 0 })) != 0 {
		t.Fatal("expected no winners")
	}
}
