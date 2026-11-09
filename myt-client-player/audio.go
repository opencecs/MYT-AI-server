//go:build windows

package main

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/hajimehoshi/oto/v2"
)

// ─── AudioDecoder ────────────────────────────────────────────────────────────

// AudioDecoder wraps FFmpeg AAC decoding (via ffdecode.dll) and oto playback.
type AudioDecoder struct {
	mu          sync.Mutex
	decoder     uintptr // FF_CreateAACDecoder handle
	buffer      *audioBuffer
	ctx         *oto.Context
	player      oto.Player
	playWg      sync.WaitGroup // waits for Play() goroutine to exit
	sampleRate  int
	channels    int
	initialized bool
	started     bool // true after Play() has been called
	outBuf      []byte // reusable decode output buffer
	adtsBuf     []byte // reusable ADTS wrapper buffer (7-byte header + AAC data)
	frameCount      int  // decoded frame count (for diagnostics)
	decodeFailures  int  // decode returned 0 count (for diagnostics)
}

// NewAudioDecoder creates a new AudioDecoder.
func NewAudioDecoder() *AudioDecoder {
	return &AudioDecoder{}
}

// Initialize sets up the AAC decoder and audio playback.
func (a *AudioDecoder) Initialize(sampleRate, channels int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.sampleRate = sampleRate
	a.channels = channels

	if err := loadFFDecode(); err != nil {
		return err
	}
	if ffProcCreateAACDecoder == 0 {
		return fmt.Errorf("ffdecode.dll missing AAC decoder exports")
	}

	// Create AAC decoder via ffdecode.dll
	handle, _, _ := purego.SyscallN(ffProcCreateAACDecoder, uintptr(sampleRate), uintptr(channels))
	if handle == 0 {
		return fmt.Errorf("FF_CreateAACDecoder failed")
	}
	a.decoder = handle

	// Initialize oto audio context
	a.buffer = newAudioBuffer()
	otoCtx, ready, err := oto.NewContext(sampleRate, channels, 2) // 2 = FormatSignedInt16LE
	if err != nil {
		purego.SyscallN(ffProcDestroyAACDecoder, handle)
		a.decoder = 0
		return fmt.Errorf("oto init: %v", err)
	}
	// Wait for oto context with timeout (can hang if audio device unavailable)
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		purego.SyscallN(ffProcDestroyAACDecoder, handle)
		a.decoder = 0
		return fmt.Errorf("oto init: context ready timeout")
	}
	a.ctx = otoCtx

	// Create player from buffer (pull model).
	// Do NOT call Play() here — on Windows, Play() blocks synchronously
	// reading from the buffer to fill its internal buffer, but no data
	// exists yet. Play() will be called after the first decode.
	a.player = a.ctx.NewPlayer(a.buffer)
	// Set a small buffer size so Play() doesn't need much data to start
	type bufferSizeSetter interface{ SetBufferSize(int) }
	if bs, ok := a.player.(bufferSizeSetter); ok {
		bs.SetBufferSize(4096)
	}

	a.initialized = true
	logf("[audio] AAC decoder initialized: %dHz %dch (FFmpeg via ffdecode.dll + oto)\n", sampleRate, channels)
	return nil
}

// DecodeAndPlay decodes AAC data and plays it through oto.
func (a *AudioDecoder) DecodeAndPlay(data []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.initialized || a.decoder == 0 {
		return
	}

	// Wrap raw AAC data in ADTS header so FFmpeg can parse it
	adtsData := a.wrapADTS(data)

	// Decode via FF_DecodeAAC
	if a.outBuf == nil {
		a.outBuf = make([]byte, 8192) // max 2048 samples * 2ch * 2 bytes
	}
	ret, _, _ := purego.SyscallN(ffProcDecodeAAC,
		a.decoder,
		uintptr(unsafe.Pointer(&adtsData[0])),
		uintptr(len(adtsData)),
		uintptr(unsafe.Pointer(&a.outBuf[0])),
		uintptr(len(a.outBuf)),
	)

	pcmSize := int(int32(ret))
	if pcmSize <= 0 {
		a.decodeFailures++
		if a.decodeFailures <= 3 || a.decodeFailures%100 == 0 {
			logf("[audio] decode returned 0 (failures=%d inputLen=%d)\n", a.decodeFailures, len(data))
		}
		return
	}

	// Write to buffer for oto playback
	a.buffer.Write(a.outBuf[:pcmSize])
	a.frameCount++
	if a.frameCount == 1 {
		logf("[audio] first PCM decoded: %d bytes, starting oto playback\n", pcmSize)
	} else if a.frameCount%100 == 0 {
		bufLen := 0
		if a.buffer != nil {
			bufLen = a.buffer.Buffered()
		}
		logf("[audio] decoded %d frames, lastPCM=%d bytes, ringBuf=%d bytes\n", a.frameCount, pcmSize, bufLen)
	}

	// Start playback on first successful decode (must be async — on Windows,
	// Play() blocks synchronously reading from the buffer to fill its internal
	// buffer, and we can't hold a.mu while Play() is blocking).
	if !a.started {
		a.started = true
		a.playWg.Add(1)
		go func() {
			a.player.Play()
			a.playWg.Done()
		}()
	}
}

// Close stops and cleans up the audio decoder.
func (a *AudioDecoder) Close() {
	a.mu.Lock()

	if a.decoder != 0 {
		purego.SyscallN(ffProcDestroyAACDecoder, a.decoder)
		a.decoder = 0
	}
	if a.buffer != nil {
		a.buffer.Close()
	}
	if a.player != nil {
		a.player.Pause()
	}
	a.ctx = nil
	a.initialized = false
	a.started = false

	a.mu.Unlock()

	// Wait for Play() goroutine to exit with timeout to avoid blocking Close() forever
	done := make(chan struct{})
	go func() {
		a.playWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		logf("[audio] Close: Play() goroutine did not exit in 2s\n")
	}
}

// ─── ADTS wrapper ────────────────────────────────────────────────────────────

// wrapADTS wraps a raw AAC access unit in an ADTS header.
// Uses the decoder's reusable adtsBuf to avoid per-frame allocation.
func (a *AudioDecoder) wrapADTS(aacData []byte) []byte {
	// ADTS header is 7 bytes (protection_absent=1)
	frameLen := len(aacData) + 7
	srIdx := sampleRateToIndex(a.sampleRate)
	chCfg := uint8(a.channels)

	need := 7 + len(aacData)
	if cap(a.adtsBuf) < need {
		a.adtsBuf = make([]byte, need)
	} else {
		a.adtsBuf = a.adtsBuf[:need]
	}
	adts := a.adtsBuf
	// Syncword
	adts[0] = 0xFF
	adts[1] = 0xF1 // MPEG-4, Layer 0, protection_absent=1
	// Profile=1 (AAC-LC), sampling freq index, private=0, channel config (3 bits)
	adts[2] = 0x40 | (srIdx << 2) | (chCfg >> 2)
	// channel config (cont), original/copy, home, copyright, frame length (13 bits)
	adts[3] = ((chCfg & 0x03) << 6) | uint8((frameLen>>11)&0x1F)
	adts[4] = uint8((frameLen >> 3) & 0xFF)
	adts[5] = uint8((frameLen & 0x07) << 5)
	adts[6] = 0xFC // buffer_fullness=0x7FF, no subsequent frames
	copy(adts[7:], aacData)
	return adts
}
