package downscale

import (
	"context"
	"errors"
	"math"
	"sync"
)

var ErrAborted = errors.New("downscale: aborted")

type handle struct {
	m     sync.RWMutex
	abort bool
	wg    sync.WaitGroup
}

func (h *handle) Wait(ctx context.Context) error {
	complete := make(chan struct{})
	go func() {
		h.wg.Wait()
		complete <- struct{}{}
	}()
	select {
	case <-complete:
		return nil
	case <-ctx.Done():
		h.SetAbort()
		<-complete
		return ErrAborted
	}
}

func (h *handle) SetAbort() {
	if h == nil {
		return
	}

	h.m.Lock()
	h.abort = true
	h.m.Unlock()
}

func (h *handle) Aborted() bool {
	if h == nil {
		return false
	}

	h.m.RLock()
	abort := h.abort
	h.m.RUnlock()
	return abort
}

func (h *handle) Done() {
	if h == nil {
		return
	}
	h.wg.Done()
}

func gcd(a uint32, b uint32) uint32 {
	if a == 0 {
		return b
	}
	for b != 0 {
		if a > b {
			a -= b
		} else {
			b -= a
		}
	}
	return a
}

func lcm(a uint32, b uint32) uint32 {
	return (a * b) / gcd(a, b)
}

func makeTable(l uint32, slcmlen uint32, dlcmlen uint32) ([]uint32, []uint32) {
	tt := make([]uint32, l+1)
	ft := make([]uint32, l+1)
	for i := uint32(0); i <= l; i++ {
		ft[i] = (dlcmlen * (i + 1)) % slcmlen
		tt[i] = (dlcmlen * i) / slcmlen
	}
	return tt, ft
}

func makeGammaTable(g float64) ([256]uint16, [65536]uint8) {
	var t [256]uint16
	for i := range t {
		t[i] = uint16(math.Pow(float64(i)/255, g) * 65535)
	}

	g = 1.0 / g
	var rt [65536]uint8
	for i := range rt {
		rt[i] = uint8(math.Pow(float64(i)/65535, g) * 255)
	}
	return t, rt
}
