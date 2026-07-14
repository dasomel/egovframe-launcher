// Package logbuf is a bounded in-memory log with fan-out to live subscribers.
// Append never blocks: a subscriber whose buffer is full simply drops the line.
package logbuf

import "sync"

type Buf struct {
	mu    sync.Mutex
	cap   int
	lines []string
	subs  map[int]chan string
	next  int
}

func New(capacity int) *Buf {
	if capacity < 1 {
		capacity = 1
	}
	return &Buf{cap: capacity, subs: map[int]chan string{}}
}

func (b *Buf) Append(line string) {
	b.mu.Lock()
	b.lines = append(b.lines, line)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}
	subs := make([]chan string, 0, len(b.subs))
	for _, ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		safeSend(ch, line)
	}
}

// safeSend delivers v to ch without blocking; it recovers from a send on a
// channel that a concurrent cancel() has already closed.
func safeSend(ch chan string, v string) {
	defer func() { _ = recover() }()
	select {
	case ch <- v:
	default:
	}
}

func (b *Buf) Snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

func (b *Buf) LastLine() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) == 0 {
		return ""
	}
	return b.lines[len(b.lines)-1]
}

func (b *Buf) Subscribe() (<-chan string, func()) {
	b.mu.Lock()
	id := b.next
	b.next++
	ch := make(chan string, 256)
	b.subs[id] = ch
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}
