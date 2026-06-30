package queue

import (
	"path/filepath"
	"testing"
)

func TestAddLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "queue.json")
	q, _ := Load(p)
	q.Add(Item{Prompt: "fix the flaky test", Repo: "github.com/me/my-svc"})
	q.Add(Item{Prompt: "add metrics", Repo: "github.com/me/web-app"})
	if err := q.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := Load(p)
	if len(got.Items) != 2 || got.Items[0].Prompt != "fix the flaky test" {
		t.Fatalf("items = %+v", got.Items)
	}
}

func TestRemoveByIndex(t *testing.T) {
	q := &Queue{Items: []Item{{Prompt: "a"}, {Prompt: "b"}, {Prompt: "c"}}}
	q.Remove(1) // remove "b"
	if len(q.Items) != 2 || q.Items[0].Prompt != "a" || q.Items[1].Prompt != "c" {
		t.Fatalf("after remove: %+v", q.Items)
	}
	q.Remove(99) // out of range = no-op
	if len(q.Items) != 2 {
		t.Fatal("out-of-range remove should be a no-op")
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	q, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Items) != 0 {
		t.Fatal("missing file → empty queue")
	}
}
