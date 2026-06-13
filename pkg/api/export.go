package api

import "time"

// SessionExportVersion is the current export schema version.
const SessionExportVersion = "1.0"

// SessionExport is a portable snapshot of a session with all messages and turns.
type SessionExport struct {
	Version    string    `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
	Session    Session   `json:"session"`
	Messages   []Message `json:"messages"`
	Turns      []Turn    `json:"turns"`
}
