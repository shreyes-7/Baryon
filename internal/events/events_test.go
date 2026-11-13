package events

import (
	"sync"
	"testing"
	"time"
)

func TestEventBusNonBlocking(t *testing.T) {
	// Small capacity of 5
	eb := NewEventBus(5)
	defer eb.Close()

	// Fill buffer completely
	for i := 0; i < 5; i++ {
		eb.EmitRequest(RequestEvent{
			Time:      time.Now(),
			ClientIP:  "192.168.1.1",
			BackendID: "backend-1",
		})
	}

	// This 6th emit must NOT block even though channel is full
	done := make(chan bool)
	go func() {
		eb.EmitRequest(RequestEvent{
			Time:      time.Now(),
			ClientIP:  "192.168.1.2",
			BackendID: "backend-2",
		})
		done <- true
	}()

	select {
	case <-done:
		// Succeeded immediately without blocking
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("EmitRequest blocked on full channel")
	}

	// Read out the 5 events
	count := 0
	for i := 0; i < 5; i++ {
		select {
		case <-eb.RequestChannel():
			count++
		default:
		}
	}
	if count != 5 {
		t.Errorf("expected 5 buffered events, read %d", count)
	}
}

func TestEventBusConcurrentEmits(t *testing.T) {
	eb := NewEventBus(100)
	var wg sync.WaitGroup

	const senders = 20
	const msgsPerSender = 50

	wg.Add(senders)
	for i := 0; i < senders; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < msgsPerSender; j++ {
				eb.EmitRequest(RequestEvent{
					Time:      time.Now(),
					ClientIP:  "10.0.0.1",
					BackendID: "backend-1",
				})
				eb.EmitNodeStatus(NodeStatusEvent{
					Time:      time.Now(),
					BackendID: "backend-1",
					Alive:     true,
				})
			}
		}(i)
	}

	// Drainer in background
	drainDone := make(chan bool)
	go func() {
		reqClosed := false
		statusClosed := false
		for !reqClosed || !statusClosed {
			select {
			case _, ok := <-eb.RequestChannel():
				if !ok {
					reqClosed = true
				}
			case _, ok := <-eb.StatusChannel():
				if !ok {
					statusClosed = true
				}
			}
		}
		drainDone <- true
	}()

	wg.Wait()
	eb.Close()
	<-drainDone
}

