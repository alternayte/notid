package federation

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// SendPersonDelete tells the peers that hold a person's content, and the
// communities the person follows, that the person is gone. It sends now,
// inside the deletion's transaction: the key is erased with the account, so a
// queued delivery could not sign. A peer that is down is logged and missed.
func (s *Server) SendPersonDelete(ctx context.Context, tx pgx.Tx, person actors.Actor) {
	q := federationdb.New(tx)
	targets, err := q.PersonCommunities(ctx, person.ID)
	if err != nil {
		s.Log.ErrorContext(ctx, "person communities", "person", person.ApID, "err", err)
		return
	}
	delID, err := newActivityID(person.ApID)
	if err != nil {
		s.Log.ErrorContext(ctx, "delete activity id", "person", person.ApID, "err", err)
		return
	}
	del := map[string]any{
		"@context": activityStreams, "id": delID, "type": "Delete", "actor": person.ApID,
		"to": []string{public}, "object": map[string]any{"id": person.ApID, "type": "Tombstone"},
	}
	sent := make(map[string]bool, len(targets))
	blocked := make(map[string]bool, len(targets))
	send := func(inbox string, act map[string]any) {
		// One copy of an activity per inbox: two communities can share a peer.
		key := inbox + " " + act["id"].(string)
		if inbox == "" || sent[key] {
			return
		}
		host := hostOf(inbox)
		b, seen := blocked[host]
		if !seen {
			p, err := q.PeerByDomain(ctx, host)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				s.Log.ErrorContext(ctx, "peer", "peer", host, "err", err)
				return
			}
			b = err == nil && p.Blocked
			blocked[host] = b
		}
		if b {
			return
		}
		sent[key] = true
		body, err := json.Marshal(act)
		if err != nil {
			s.Log.ErrorContext(ctx, "encode person delete", "err", err)
			return
		}
		if err := s.post(ctx, DeliverArgs{Inbox: inbox, Activity: body, SignerID: person.ID}); err != nil {
			s.Log.WarnContext(ctx, "person delete not delivered", "inbox", inbox, "err", err)
		}
	}
	for _, c := range targets {
		if !c.Local {
			send(c.Inbox, del)
			if c.FollowApID == "" {
				continue
			}
			undoID, err := newActivityID(person.ApID)
			if err != nil {
				s.Log.ErrorContext(ctx, "undo activity id", "person", person.ApID, "err", err)
				continue
			}
			send(c.Inbox, undoFollow(person, c.ApID, c.FollowApID, undoID))
			continue
		}
		inboxes, err := q.FollowerInboxes(ctx, c.ID)
		if err != nil {
			s.Log.ErrorContext(ctx, "follower inboxes", "community", c.ApID, "err", err)
			continue
		}
		for _, inbox := range inboxes {
			send(inbox, del)
		}
	}
}

// undoFollow withdraws a person's follow of a remote group.
func undoFollow(person actors.Actor, groupApID, followApID, id string) map[string]any {
	return map[string]any{
		"@context": activityStreams, "id": id, "type": "Undo", "actor": person.ApID,
		"to": []string{groupApID}, "audience": groupApID,
		"object": map[string]any{
			"id": followApID, "type": "Follow", "actor": person.ApID, "object": groupApID,
		},
	}
}
