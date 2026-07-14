package logbuf

import "testing"

func TestAppendSnapshotRingDrop(t *testing.T) {
	b := New(3)
	for _, s := range []string{"a", "b", "c", "d"} {
		b.Append(s)
	}
	got := b.Snapshot()
	if len(got) != 3 || got[0] != "b" || got[2] != "d" {
		t.Fatalf("ring not enforced: %v", got)
	}
}

func TestSubscribeReceivesNewLines(t *testing.T) {
	b := New(10)
	ch, cancel := b.Subscribe()
	defer cancel()
	b.Append("hello")
	select {
	case got := <-ch:
		if got != "hello" {
			t.Fatalf("got %q", got)
		}
	default:
		t.Fatal("subscriber received nothing")
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	b := New(10)
	ch, cancel := b.Subscribe()
	cancel()
	b.Append("x")
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after cancel")
	}
}

func TestAppendDoesNotBlockOnSlowSubscriber(t *testing.T) {
	b := New(10)
	_, cancel := b.Subscribe() // never drained
	defer cancel()
	for i := 0; i < 1000; i++ {
		b.Append("flood") // must not deadlock
	}
}

func TestConcurrentCancelAndAppendNoPanic(t *testing.T) {
	b := New(10)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			b.Append("x")
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		_, cancel := b.Subscribe()
		cancel()
	}
	<-done
}
