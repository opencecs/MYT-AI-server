package main

import (
	"io"
	"sync"
)

// ─── AudioBuffer — ring buffer bridging push (decoder) to pull (oto io.Reader) ─

// Max audio buffer size — must be a power of 2 for bitwise modulo.
// Approx 1 second of stereo 16-bit 44100Hz = 176400 bytes; 256KB = 2^18.
const audioBufMaxSize = 256 * 1024

// audioBufMask is the bitmask for modulo: i % audioBufMaxSize == i & audioBufMask
const audioBufMask = audioBufMaxSize - 1

type audioBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	ring   []byte
	head   int  // read position
	tail   int  // write position
	count  int  // bytes in buffer
	closed bool
}

func newAudioBuffer() *audioBuffer {
	ab := &audioBuffer{
		ring: make([]byte, audioBufMaxSize),
	}
	ab.cond = sync.NewCond(&ab.mu)
	return ab
}

func (ab *audioBuffer) Write(data []byte) {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	if ab.closed {
		return
	}

	// Drop oldest data if buffer would overflow (O(1) instead of O(n) per-byte loop)
	if avail := len(ab.ring) - ab.count; len(data) > avail {
		drop := len(data) - avail
		ab.head = (ab.head + drop) & audioBufMask
		ab.count -= drop
	}

	// Copy data into ring buffer (may wrap around)
	n := copy(ab.ring[ab.tail:], data)
	if n < len(data) {
		copy(ab.ring, data[n:])
	}
	ab.tail = (ab.tail + len(data)) & audioBufMask
	ab.count += len(data)
	ab.cond.Signal()
}

func (ab *audioBuffer) Read(out []byte) (int, error) {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	for ab.count == 0 {
		if ab.closed {
			return 0, io.EOF
		}
		ab.cond.Wait()
	}

	n := min(len(out), ab.count)
	// Read from ring buffer (may wrap around)
	read1 := min(n, len(ab.ring)-ab.head)
	copy(out, ab.ring[ab.head:ab.head+read1])
	if read1 < n {
		copy(out[read1:], ab.ring[:n-read1])
	}
	ab.head = (ab.head + n) & audioBufMask
	ab.count -= n
	return n, nil
}

func (ab *audioBuffer) Close() {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	ab.closed = true
	ab.cond.Broadcast()
}

// Buffered returns the number of bytes currently in the ring buffer.
func (ab *audioBuffer) Buffered() int {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	return ab.count
}

// Clear clears all queued data under ab.mu (does not change closed state;
// waiters in Read keep waiting on count==0). Equivalent to upstream
// SDL_ClearQueuedAudio: drop backlog over 250ms to keep latency low.
// Taking the lock here (instead of requiring the caller to hold it) is
// mandatory: the oto pull goroutine reads concurrently, and mutating
// head/tail/count without ab.mu corrupts the ring (torn reads, negative
// count) - the root cause of the tebox crackling noise.
func (ab *audioBuffer) Clear() {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	ab.head = 0
	ab.tail = 0
	ab.count = 0
	ab.cond.Broadcast()
}

func sampleRateToIndex(sr int) uint8 {
	switch sr {
	case 96000:
		return 0
	case 88200:
		return 1
	case 64000:
		return 2
	case 48000:
		return 3
	case 44100:
		return 4
	case 32000:
		return 5
	case 24000:
		return 6
	case 22050:
		return 7
	case 16000:
		return 8
	case 12000:
		return 9
	case 11025:
		return 10
	case 8000:
		return 11
	default:
		return 15 // explicit
	}
}
