//go:build linux

package main

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/oto/v3"
	"github.com/ebitengine/purego"
)

// AudioDecoder wraps FFmpeg AAC decoding (via libffdecode.so) and oto/v3 playback.
// oto v3 在 Linux 走 ALSA（driver_unix.go，#cgo pkg-config: alsa）。
type AudioDecoder struct {
	mu          sync.Mutex
	decoder     uintptr
	buffer      *audioBuffer
	ctx         *oto.Context
	player      *oto.Player
	playWg      sync.WaitGroup
	sampleRate  int
	channels    int
	initialized bool
	started     bool
	outBuf      []byte
	adtsBuf     []byte
}

func NewAudioDecoder() *AudioDecoder { return &AudioDecoder{} }

func (a *AudioDecoder) Initialize(sampleRate, channels int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.sampleRate = sampleRate
	a.channels = channels

	if err := loadFFDecode(); err != nil {
		return err
	}
	if ffProcCreateAACDecoder == 0 {
		return fmt.Errorf("libffdecode.so missing AAC decoder exports")
	}

	handle, _, _ := purego.SyscallN(ffProcCreateAACDecoder, uintptr(sampleRate), uintptr(channels))
	if handle == 0 {
		return fmt.Errorf("FF_CreateAACDecoder failed")
	}
	a.decoder = handle

	a.buffer = newAudioBuffer()
	otoCtx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		purego.SyscallN(ffProcDestroyAACDecoder, handle)
		a.decoder = 0
		return fmt.Errorf("oto init: %v", err)
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		purego.SyscallN(ffProcDestroyAACDecoder, handle)
		a.decoder = 0
		return fmt.Errorf("oto init: context ready timeout")
	}
	a.ctx = otoCtx

	a.player = a.ctx.NewPlayer(a.buffer)
	a.player.SetBufferSize(4096)

	a.initialized = true
	logf("[audio] AAC decoder initialized: %dHz %dch (FFmpeg via libffdecode.so + oto/v3 ALSA)\n", sampleRate, channels)
	return nil
}

func (a *AudioDecoder) DecodeAndPlay(data []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.initialized || a.decoder == 0 {
		return
	}

	adtsData := a.wrapADTS(data)

	if a.outBuf == nil {
		a.outBuf = make([]byte, 8192)
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
		return
	}

	a.buffer.Write(a.outBuf[:pcmSize])

	if !a.started {
		a.started = true
		a.playWg.Add(1)
		go func() {
			a.player.Play()
			a.playWg.Done()
		}()
	}
}

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

func (a *AudioDecoder) wrapADTS(aacData []byte) []byte {
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
	adts[0] = 0xFF
	adts[1] = 0xF1
	adts[2] = 0x40 | (srIdx << 2) | (chCfg >> 2)
	adts[3] = ((chCfg & 0x03) << 6) | uint8((frameLen>>11)&0x1F)
	adts[4] = uint8((frameLen >> 3) & 0xFF)
	adts[5] = uint8((frameLen & 0x07) << 5)
	adts[6] = 0xFC
	copy(adts[7:], aacData)
	return adts
}
