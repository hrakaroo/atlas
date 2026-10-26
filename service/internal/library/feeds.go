package library

import "errors"

// Seed older editions deterministically; subsequent imports record their arrival
// in the same transaction as the item. Reads never acknowledge feed attention.
func (l *Library) syncFeedArrivals() error {
	if err := l.exec(`INSERT OR IGNORE INTO feed_arrivals(item_id,feed_id)
        SELECT m.item_id,m.feed_id FROM blog_members m JOIN items i ON i.id=m.item_id
        WHERE NOT EXISTS (SELECT 1 FROM feed_arrivals a WHERE a.item_id=m.item_id)
        ORDER BY i.created,i.id`); err != nil {
		return err
	}
	return l.exec(`INSERT OR IGNORE INTO feed_events(item_id,feed_id,event_key)
        SELECT item_id,feed_id,'arrival:'||item_id FROM feed_arrivals
        WHERE NOT EXISTS(SELECT 1 FROM feed_events e WHERE e.event_key='arrival:'||feed_arrivals.item_id)
        ORDER BY id`)
}
func (l *Library) feedAttention(id string) (Record, error) {
	return l.row(`SELECT coalesce(max(a.id),0) AS latest_entry_id,
        count(DISTINCT CASE WHEN a.id>coalesce(s.through_id,0) THEN a.item_id END) AS unseen_entries
        FROM feed_events a JOIN items i ON i.id=a.item_id
        LEFT JOIN feed_seen s ON s.feed_id=a.feed_id
        WHERE a.feed_id=? AND i.deleted_at IS NULL`, id)
}
func (l *Library) MarkFeedSeen(id string, through int64) (Record, error) {
	if through <= 0 {
		return nil, errors.New("Invalid feed arrival cursor.")
	}
	var attention Record
	err := l.transaction(func() error {
		if err := l.syncFeedArrivals(); err != nil {
			return err
		}
		row, err := l.row("SELECT id FROM feed_events WHERE feed_id=? AND id=?", id, through)
		if err != nil {
			return err
		}
		if row == nil {
			return errors.New("Feed arrival not found.")
		}
		if err = l.exec("INSERT INTO feed_seen VALUES(?,?) ON CONFLICT(feed_id) DO UPDATE SET through_id=max(through_id,excluded.through_id)", id, through); err != nil {
			return err
		}
		attention, err = l.feedAttention(id)
		return err
	})
	if err != nil {
		return nil, err
	}
	attention["feed_id"] = id
	return attention, nil
}
