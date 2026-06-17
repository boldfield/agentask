package notify

import "context"

type Notification struct {
	Event    string   `json:"event"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Link     string   `json:"link"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags"`
	DedupKey string   `json:"dedup_key"`
}

type Notifier interface {
	Publish(ctx context.Context, n Notification) error
}
