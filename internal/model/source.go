package model

import "time"

type Source struct {
	Commit ObjectID
	Tree   ObjectID
	Base   ObjectID
}

type Target struct {
	Name     string
	Portfile string
	Subport  string
	Variants map[string]bool
}

type Platform struct {
	OS           string
	Version      string
	Architecture string
}

type Revision struct {
	ID        RevisionID
	ChangeID  ChangeID
	Previous  RevisionID
	Source    Source
	CreatedAt time.Time
}

type Artifact struct {
	Name      string
	Digest    string
	Location  string
	MediaType string
}
