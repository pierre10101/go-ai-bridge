package domain

// SectionOnEvent is one section on a list page, with its event's title.
//
// bridge-en: a section on an event
type SectionOnEvent struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Capacity int64  `json:"capacity"`
	Title    string `json:"title"`
}
