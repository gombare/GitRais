package app

import "gitrais/internal/graph"

// TabInfo identifies an open repository tab.
type TabInfo struct {
	ID   string `json:"id"`
	Path string `json:"path"` // repository top-level directory (symlinks resolved)
	Name string `json:"name"` // base name of Path
}

// RowDTO is one graph row as sent to the frontend. Up and Down are the lane
// segments drawn in the upper and lower half of the row, as [from, to, color].
type RowDTO struct {
	Hash     string   `json:"hash"`
	Parents  []string `json:"parents"`
	Kind     string   `json:"kind"` // "commit" | "stash"
	Lane     int      `json:"lane"`
	Color    int      `json:"color"`
	Up       [][3]int `json:"up"`
	Down     [][3]int `json:"down"`
	Author   string   `json:"author"`
	Email    string   `json:"email"`
	Time     int64    `json:"time"` // unix seconds
	Subject  string   `json:"subject"`
	StashRef string   `json:"stashRef"` // "stash@{N}" for stash rows, "" otherwise
}

type GraphInfo struct {
	Rows int  `json:"rows"`
	Done bool `json:"done"`
}

// AuthPrompt is the payload of the auth:prompt event.
type AuthPrompt struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Secret bool   `json:"secret"` // true for passwords/passphrases/tokens: mask the input
}

// AuthCancel is the payload of the auth:cancel event: the prompt with this ID
// is no longer pending (helper died, operation cancelled, timeout, shutdown)
// and its dialog must be closed without answering.
type AuthCancel struct {
	ID string `json:"id"`
}

func segs(s []graph.Seg) [][3]int {
	out := make([][3]int, len(s))
	for i, x := range s {
		out[i] = [3]int{int(x.From), int(x.To), int(x.Color)}
	}
	return out
}

func toRowDTO(r graph.Row) RowDTO {
	kind := "commit"
	if r.Kind == graph.RowStash {
		kind = "stash"
	}
	parents := r.Parents
	if parents == nil {
		parents = []string{}
	}
	return RowDTO{Hash: r.Hash, Parents: parents, Kind: kind, Lane: r.Lane, Color: r.Color,
		Up: segs(r.Up), Down: segs(r.Down), Author: r.Author, Email: r.Email, Time: r.Time,
		Subject: r.Subject, StashRef: r.StashRef}
}
