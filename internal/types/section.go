package types

import (
	"errors"
	"time"
)

// OSM Profile Response Types
type OSMSection struct {
	SectionName string    `json:"section_name"`
	GroupName   string    `json:"group_name"`
	SectionID   int       `json:"section_id"`
	GroupID     int       `json:"group_id"`
	SectionType string    `json:"section_type"`
	Terms       []OSMTerm `json:"terms"`
}

var ErrNotInActiveTerm = errors.New("not in an active term")

func (s OSMSection) GetCurrentTerm() (*OSMTerm, error) {
	searchDate := time.Now()
	for _, term := range s.Terms {
		if term.Contains(searchDate) {
			return &term, nil
		}
	}
	return nil, ErrNotInActiveTerm
}
