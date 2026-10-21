package db

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestBlueEventSourceCursorAndConfiguration(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	name := "source-" + uuid.NewString()
	source, err := d.CreateBlueEventSource(ctx, BlueEventSource{
		Name: name, URL: "http://logs.example.test/events", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.DeleteBlueEventSource(context.Background(), source.ID) })
	if source.PollIntervalSeconds != 30 || source.Cursor != "" || source.LastSyncedAt != nil || !source.Enabled {
		t.Fatalf("source defaults: %+v", source)
	}
	if _, err := d.CreateBlueEventSource(ctx, BlueEventSource{Name: name, URL: "http://elsewhere.test", Enabled: true}); err == nil {
		t.Fatal("duplicate source name accepted")
	}
	listed, err := d.ListBlueEventSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listed {
		if item.ID == source.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("created source missing from list")
	}

	advanced, err := d.RecordBlueEventSourceSync(ctx, source.ID, "", source.URL, source.UpdatedAt, "cursor-1", "")
	if err != nil || !advanced {
		t.Fatalf("record first sync: advanced=%v err=%v", advanced, err)
	}
	stored, err := d.GetBlueEventSource(ctx, source.ID)
	if err != nil || stored == nil || stored.Cursor != "cursor-1" || stored.LastSyncedAt == nil || stored.LastError != "" {
		t.Fatalf("first sync state: %+v err=%v", stored, err)
	}
	firstSyncAt := *stored.LastSyncedAt
	advanced, err = d.RecordBlueEventSourceSync(ctx, source.ID, "", source.URL, source.UpdatedAt, "stale-cursor", "")
	if err != nil || advanced {
		t.Fatalf("stale cursor advanced: advanced=%v err=%v", advanced, err)
	}
	advanced, err = d.RecordBlueEventSourceSync(ctx, source.ID, "cursor-1", source.URL, stored.UpdatedAt, "", "upstream unavailable")
	if err != nil || !advanced {
		t.Fatalf("record failed sync: advanced=%v err=%v", advanced, err)
	}
	stored, err = d.GetBlueEventSource(ctx, source.ID)
	if err != nil || stored == nil || stored.Cursor != "cursor-1" || stored.LastError != "upstream unavailable" ||
		stored.LastSyncedAt == nil || !stored.LastSyncedAt.Equal(firstSyncAt) {
		t.Fatalf("failure should preserve last successful cursor/time: %+v err=%v", stored, err)
	}
	// Another server can change a setting while a fetch is in flight without
	// changing its URL or cursor. The old fetch must not save a result then.
	staleSnapshot := *stored
	newName := name + "-renamed"
	stored, err = d.PatchBlueEventSource(ctx, source.ID, BlueEventSourcePatch{Name: &newName})
	if err != nil || stored == nil || stored.Name != newName || stored.Cursor != staleSnapshot.Cursor {
		t.Fatalf("rename source: %+v err=%v", stored, err)
	}
	advanced, err = d.RecordBlueEventSourceSync(ctx, source.ID, staleSnapshot.Cursor, staleSnapshot.URL,
		staleSnapshot.UpdatedAt, "old-fetch-cursor", "")
	if err != nil || advanced {
		t.Fatalf("stale configuration advanced cursor: advanced=%v err=%v", advanced, err)
	}

	newURL := "https://new-feed.example.test/logs"
	disabled := false
	interval := 60
	stored, err = d.PatchBlueEventSource(ctx, source.ID, BlueEventSourcePatch{
		URL: &newURL, Enabled: &disabled, PollIntervalSeconds: &interval,
	})
	if err != nil || stored == nil || stored.URL != newURL || stored.Enabled || stored.PollIntervalSeconds != 60 ||
		stored.Cursor != "" || stored.LastSyncedAt != nil || stored.LastError != "" {
		t.Fatalf("URL edit must reset sync position: %+v err=%v", stored, err)
	}
	advanced, err = d.RecordBlueEventSourceSync(ctx, source.ID, "", source.URL, source.UpdatedAt, "old-feed-cursor", "")
	if err != nil || advanced {
		t.Fatalf("old URL advanced new feed: advanced=%v err=%v", advanced, err)
	}
	advanced, err = d.RecordBlueEventSourceSync(ctx, source.ID, "", newURL, stored.UpdatedAt, "cursor-2", "")
	if err != nil || !advanced {
		t.Fatalf("new feed did not advance: advanced=%v err=%v", advanced, err)
	}
	stored, err = d.PatchBlueEventSource(ctx, source.ID, BlueEventSourcePatch{ResetCursor: true})
	if err != nil || stored == nil || stored.Cursor != "" || stored.LastSyncedAt != nil {
		t.Fatalf("explicit cursor reset: %+v err=%v", stored, err)
	}
	deleted, err := d.DeleteBlueEventSource(ctx, source.ID)
	if err != nil || !deleted {
		t.Fatalf("delete source: deleted=%v err=%v", deleted, err)
	}
	stored, err = d.GetBlueEventSource(ctx, source.ID)
	if err != nil || stored != nil {
		t.Fatalf("deleted source remains: %+v err=%v", stored, err)
	}
}
