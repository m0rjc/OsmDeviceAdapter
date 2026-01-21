package types

import "errors"

type OSMProfileData struct {
	UserID           int          `json:"user_id"`
	FullName         string       `json:"full_name"`
	Email            string       `json:"email"`
	Sections         []OSMSection `json:"sections"`
	HasParentAccess  bool         `json:"has_parent_access"`
	HasSectionAccess bool         `json:"has_section_access"`
}

var ErrCannotFindSection = errors.New("cannot find section")

func (p OSMProfileData) GetCurrentTermForSection(sectionId int) (*OSMTerm, error) {
	section, err := p.GetSection(sectionId)
	if err != nil {
		return nil, err
	}
	term, err := section.GetCurrentTerm()
	if err != nil {
		return nil, err
	}
	return term, nil
}

func (p OSMProfileData) GetSection(sectionId int) (*OSMSection, error) {
	for _, section := range p.Sections {
		if section.SectionID == sectionId {
			return &section, nil
		}
	}
	return nil, ErrCannotFindSection
}

type OSMProfileResponse struct {
	Status bool            `json:"status"`
	Error  *string         `json:"error"`
	Data   *OSMProfileData `json:"data"`
}
