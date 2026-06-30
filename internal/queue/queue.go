// Package queue is the client-only staging list of tasks to dispatch. It lives
// entirely on the laptop (~/.rbg/queue.json) and never touches the desktop until
// the user dispatches an item.
package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Item is one staged task: a prompt and the repo to run it against.
type Item struct {
	Prompt string `json:"prompt"`
	Repo   string `json:"repo"`
}

// Queue is the ordered list of staged items, persisted to path.
type Queue struct {
	path  string
	Items []Item `json:"items"`
}

// Load reads the queue at path; a missing or corrupt file yields an empty queue.
func Load(path string) (*Queue, error) {
	q := &Queue{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return q, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(data, q) // corrupt → empty
	return q, nil
}

// Add appends an item.
func (q *Queue) Add(it Item) { q.Items = append(q.Items, it) }

// Remove deletes the item at index i (out-of-range is a no-op).
func (q *Queue) Remove(i int) {
	if i < 0 || i >= len(q.Items) {
		return
	}
	q.Items = append(q.Items[:i], q.Items[i+1:]...)
}

// Save writes the queue atomically (temp + rename), creating parents.
func (q *Queue) Save() error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Items []Item `json:"items"`
	}{q.Items}, "", "  ")
	if err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}
