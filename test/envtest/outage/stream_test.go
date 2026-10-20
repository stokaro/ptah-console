package outage_test

import (
	"time"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

func isHeartbeat(event *consolev1.SubscribeResponse) bool { return event.GetHeartbeat() != nil }

func isReconnecting(event *consolev1.SubscribeResponse) bool {
	return event.GetLost().GetReconnecting()
}

// isWholeKind is the invalidation that tells the page to read a kind again.
func isWholeKind(event *consolev1.SubscribeResponse) bool {
	return event.GetInvalidate() != nil && event.GetInvalidate().GetName() == ""
}

// events reads a stream on one goroutine, so a test can wait for an event or
// for a while without losing what arrives in between.
type events struct {
	next <-chan *consolev1.SubscribeResponse
	done <-chan error
}

func read(stream consolev1connect.WatchServiceSubscribeClientStream) events {
	next := make(chan *consolev1.SubscribeResponse, 1024)
	done := make(chan error, 1)
	go func() {
		for {
			event, err := stream.Receive()
			if err != nil {
				done <- err
				return
			}
			next <- event
		}
	}()
	return events{next: next, done: done}
}

// until collects events until one matches.
func (e events) until(match func(*consolev1.SubscribeResponse) bool, wait time.Duration) ([]*consolev1.SubscribeResponse, bool) {
	var seen []*consolev1.SubscribeResponse
	deadline := time.After(wait)
	for {
		select {
		case event := <-e.next:
			seen = append(seen, event)
			if match(event) {
				return seen, true
			}
		case <-e.done:
			return seen, false
		case <-deadline:
			return seen, false
		}
	}
}

// during collects whatever arrives for a while.
func (e events) during(wait time.Duration) []*consolev1.SubscribeResponse {
	var seen []*consolev1.SubscribeResponse
	deadline := time.After(wait)
	for {
		select {
		case event := <-e.next:
			seen = append(seen, event)
		case <-e.done:
			return seen
		case <-deadline:
			return seen
		}
	}
}

func count(events []*consolev1.SubscribeResponse, match func(*consolev1.SubscribeResponse) bool) int {
	n := 0
	for _, event := range events {
		if match(event) {
			n++
		}
	}
	return n
}

// lostTwice matches the second reconnecting loss of any one kind, counting
// losses per kind in seen.
func lostTwice(seen map[consolev1.WatchedKind]int) func(*consolev1.SubscribeResponse) bool {
	return func(event *consolev1.SubscribeResponse) bool {
		if !isReconnecting(event) {
			return false
		}
		seen[event.GetLost().GetKind()]++
		return seen[event.GetLost().GetKind()] > 1
	}
}
