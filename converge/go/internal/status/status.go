// Package status maintains a per-session JSON snapshot file so external
// watchers (tail -F, another agent, the user) can see what the converge loop
// is currently doing.
package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Snapshot is the on-disk format. Stored at
// ${CONVERGE_STATUS_DIR:-/tmp}/converge-status-<session-id>.json
type Snapshot struct {
	SessionID    string    `json:"session_id"`
	Mode         string    `json:"mode"`
	MaxRounds    int       `json:"max_rounds"`
	Started      int64     `json:"started"`
	Updated      int64     `json:"updated"`
	Ended        int64     `json:"ended,omitempty"`
	CurrentRound int       `json:"current_round"`
	Phase        string    `json:"phase"`
	ThreadID     string    `json:"thread_id,omitempty"`
	Verdicts     []Verdict `json:"verdicts"`
	Outcome      string    `json:"outcome,omitempty"`
	Events       []Event   `json:"events"`
}

type Verdict struct {
	TS      int64  `json:"ts"`
	Round   int    `json:"round"`
	Author  string `json:"author"`
	Verdict string `json:"verdict"`
	Issues  int    `json:"issues"`
}

type Event struct {
	TS    int64  `json:"ts"`
	Round int    `json:"round"`
	Phase string `json:"phase"`
}

// Path returns the snapshot path for a given session id.
func Path(sessionID string) string {
	dir := os.Getenv("CONVERGE_STATUS_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, fmt.Sprintf("converge-status-%s.json", sessionID))
}

// Load reads the snapshot, returning an empty one if it doesn't exist yet.
func Load(sessionID string) (*Snapshot, error) {
	p := Path(sessionID)
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &Snapshot{SessionID: sessionID, Verdicts: []Verdict{}, Events: []Event{}}, nil
	}
	if err != nil {
		return nil, err
	}
	s := &Snapshot{}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	return s, nil
}

// Save persists the snapshot.
func Save(s *Snapshot) error {
	s.Updated = time.Now().Unix()
	p := Path(s.SessionID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// Start initializes a snapshot.
func Start(sessionID, mode string, maxRounds int) error {
	s, _ := Load(sessionID)
	now := time.Now().Unix()
	s.SessionID = sessionID
	s.Mode = mode
	s.MaxRounds = maxRounds
	s.Started = now
	s.CurrentRound = 0
	s.Phase = "starting"
	s.Outcome = ""
	if s.Verdicts == nil {
		s.Verdicts = []Verdict{}
	}
	if s.Events == nil {
		s.Events = []Event{}
	}
	return Save(s)
}

// Round records a phase transition for the current round.
func Round(sessionID string, round int, phase string) error {
	s, err := Load(sessionID)
	if err != nil {
		return err
	}
	s.CurrentRound = round
	s.Phase = phase
	s.Events = append(s.Events, Event{TS: time.Now().Unix(), Round: round, Phase: phase})
	return Save(s)
}

// Thread records the codex thread id (captured in round 1, used for resume).
func Thread(sessionID, threadID string) error {
	s, err := Load(sessionID)
	if err != nil {
		return err
	}
	s.ThreadID = threadID
	return Save(s)
}

// AddVerdict records one author's verdict for the current round.
func AddVerdict(sessionID, author, verdict string, issues int) error {
	s, err := Load(sessionID)
	if err != nil {
		return err
	}
	s.Verdicts = append(s.Verdicts, Verdict{
		TS:      time.Now().Unix(),
		Round:   s.CurrentRound,
		Author:  author,
		Verdict: verdict,
		Issues:  issues,
	})
	return Save(s)
}

// End finalizes the snapshot.
func End(sessionID, outcome string) error {
	s, err := Load(sessionID)
	if err != nil {
		return err
	}
	s.Phase = "done"
	s.Outcome = outcome
	s.Ended = time.Now().Unix()
	return Save(s)
}
