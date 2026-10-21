package phase

import "time"

const (
	Upcoming  = "upcoming"
	Running   = "running"
	Waiting   = "waiting"
	Announced = "announced"
	Archived  = "archived"
)

func Of(now, start, end, announce time.Time, archived bool) string {
	if archived {
		return Archived
	}
	if now.Before(start) {
		return Upcoming
	}
	if now.Before(end) {
		return Running
	}
	if now.Before(announce) {
		return Waiting
	}
	return Announced
}

func VisibleToUser(now, start time.Time, archived bool) bool {
	return !archived && !now.Before(start)
}
