package store

// TaskSummary is one line of a task list: what the stored task says, no
// purchase, event or binding detail (docs/demo-operation-design.md section 14).
type TaskSummary struct {
	TaskID     string `json:"task_id"`
	ConsumerID string `json:"consumer_id"`
	Status     string `json:"status"`
	DenyReason string `json:"deny_reason,omitempty"`
	StartedAt  uint64 `json:"started_at"`
}

// ListTasks lists stored tasks, newest first, at most limit (read only).
// consumerID "" lists every consumer's; otherwise only the tasks stored for
// that consumer, whatever the current registry says (as ReadTask decides).
func (s *Store) ListTasks(consumerID string, limit int) ([]TaskSummary, error) {
	rows, err := s.r.Query(`SELECT task_id, consumer_id, status, coalesce(deny_reason, ''), started_at FROM tasks
		WHERE ? = '' OR consumer_id = ? ORDER BY started_at DESC, task_id DESC LIMIT ?`, consumerID, consumerID, limit)
	if err != nil {
		return nil, &ReadError{Check: "tasks", Err: err}
	}
	out := []TaskSummary{}
	for rows.Next() {
		var t TaskSummary
		if err := rows.Scan(&t.TaskID, &t.ConsumerID, &t.Status, &t.DenyReason, &t.StartedAt); err != nil {
			rows.Close()
			return nil, &ReadError{Check: "tasks", Err: err}
		}
		out = append(out, t)
	}
	if err := drained(rows); err != nil {
		return nil, &ReadError{Check: "tasks", Err: err}
	}
	return out, nil
}
