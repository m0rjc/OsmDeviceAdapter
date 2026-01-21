package types

import (
	"log/slog"
	"time"
)

// OSMTerm represents a term in an OSM Profile
type OSMTerm struct {
	Name       string `json:"name"`
	StartDate  string `json:"startdate"`
	_startDate *time.Time
	EndDate    string `json:"enddate"`
	_endDate   *time.Time
	TermID     int `json:"term_id"`
}

func (t OSMTerm) GetStartDate() time.Time {
	if t._startDate == nil {
		t._startDate = t.parseOsmDate(t.StartDate)
	}
	return *t._startDate
}

func (t OSMTerm) GetEndDate() time.Time {
	if t._endDate == nil {
		t._endDate = t.parseOsmDate(t.EndDate)
	}
	return *t._endDate
}

// Contains returns true if the term contains the given date.
// Term dates are considered inclusive.
func (t OSMTerm) Contains(date time.Time) bool {
	return !date.Before(t.GetStartDate()) && !date.After(t.GetEndDate())
}

// parseOsmDate attempts to parse an OSM date string.
// It returns zero if this is not possible, having logged an error
func (t OSMTerm) parseOsmDate(date string) *time.Time {
	var result time.Time
	var err error
	result, err = time.Parse("2006-01-02", date)
	if err != nil {
		slog.Warn("osm.term_discovery.invalid_date",
			"component", "term_discovery",
			"event", "term.parse_error",
			"term_id", t.TermID,
			"start_date", date,
			"error", err,
		)
		var zero time.Time
		return &zero
	}
	return &result
}
