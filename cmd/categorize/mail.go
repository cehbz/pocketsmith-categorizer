package main

import (
	"context"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/gmail"
)

// mailbox finds the user's messages, reading in full those skip doesn't say to leave.
type mailbox interface {
	Messages(ctx context.Context, query string, from, to time.Time, skip func(id string) bool) ([]gmail.Message, error)
}

// overlap is how many days before the store's read point a run reads mail again, for
// messages that arrive late.
const overlap = 2

// readMail reads each merchant's mail into the store: from overlap days before the store's
// read point, or from start when it hasn't read any or that is earlier, to today, skipping the
// messages the store holds. Only when every search succeeds does the store keep what was
// read, its read point move to today and the messages received before start leave it, and
// is it saved; a failed search leaves it as it was.
func readMail(ctx context.Context, s *session, store *evidence.Store, start time.Time) error {
	from := start
	if since := store.ReadTo.AddDate(0, 0, -overlap); !store.ReadTo.IsZero() && since.After(start) {
		from = since
	}
	read := map[string]bool{}
	skip := func(id string) bool { return store.Holds(id) || read[id] }
	var reads []evidence.Read
	for _, m := range s.merchants {
		for _, q := range m.Queries {
			msgs, err := s.receipts.Messages(ctx, q, from, s.today, skip)
			if err != nil {
				return err
			}
			for _, msg := range msgs {
				read[msg.ID] = true
				r := evidence.Read{ID: msg.ID, Merchant: m.Name, Received: msg.Received}
				ev, err := m.Reader.Read(evidence.Message{ID: msg.ID, Received: msg.Received, From: msg.From, Subject: msg.Subject, Text: msg.Text})
				if err != nil {
					r.Unreadable = err.Error()
				} else {
					r.Evidence = ev
				}
				reads = append(reads, r)
			}
		}
	}
	for _, r := range reads {
		store.Add(r)
	}
	store.ReadTo = s.today
	store.Drop(start)
	return store.Save()
}
