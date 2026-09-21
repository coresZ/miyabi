package scan

import (
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/pan"
)

// Payload describes a library scanning task in the task queue.
type Payload struct {
	Source        domain.LibrarySource `json:"source"`
	Scan          domain.ScanProgress  `json:"scan"`
	TargetID      string               `json:"target_id,omitempty"`
	TargetPath    string               `json:"target_path,omitempty"`
	TargetFile    bool                 `json:"target_file,omitempty"`
	OfflineTaskID int                  `json:"offline_task_id,omitempty"`
	Code          string               `json:"code,omitempty"`
	JavDBID       string               `json:"javdb_id,omitempty"`
}

// Directory describes a directory queued during BFS traversal.
type Directory struct {
	ID   string
	Path string
}

// Video wraps a 115 file with an assigned catalogue code.
type Video struct {
	pan.File
	Code string
}
