package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestZstdDeltaRawDictionaryAndMalformedFrames(t *testing.T) {
	basis := make([]byte, 256<<10)
	rng := rand.New(rand.NewPCG(7, 11))
	for i := range basis {
		basis[i] = byte(rng.Uint32())
	}
	target := bytes.Clone(basis)
	copy(target[100:200], bytes.Repeat([]byte{'x'}, 100))
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderDictRaw(0, basis), zstd.WithEncoderConcurrency(1),
		zstd.WithSingleSegment(true), zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	patch := enc.EncodeAll(target, nil)
	if len(patch) >= len(target)/8 {
		t.Fatal("fixture did not depend on dictionary")
	}
	decode := func(dictionary, frame []byte) ([]byte, error) {
		r, err := openZstdDelta(dictionary, frame, int64(len(target)))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(io.LimitReader(r, int64(len(target))+1))
	}
	if got, err := decode(basis, patch); err != nil || !bytes.Equal(got, target) {
		t.Fatalf("valid dictionary decode: %v", err)
	}
	corrupt := bytes.Clone(patch)
	corrupt[len(corrupt)-1] ^= 0xff
	for _, tc := range []struct {
		name        string
		dict, frame []byte
	}{
		{"checksum", basis, corrupt},
		{"truncated", basis, patch[:len(patch)-3]},
		{"wrong dictionary", make([]byte, len(basis)), patch},
		{"missing dictionary", nil, patch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decode(tc.dict, tc.frame); err == nil {
				t.Fatal("invalid frame was accepted")
			}
		})
	}
	if _, err := openZstdDelta(basis, patch, int64(len(target)+1)); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("frame with wrong decoded length: %v", err)
	}
}

func TestZstdDeltaRejectsOversizedWindowBeforeDecoderAllocation(t *testing.T) {
	// Standard frame header with a 128 MiB window and a declared 1 KiB
	// content size. The profile must reject the window before reading a block.
	frame := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x84, 0x88, 0, 0, 0, 0, 1, 0, 0}
	binary.LittleEndian.PutUint32(frame[6:10], 1024)
	if _, err := openZstdDelta([]byte("basis"), frame, 1024); err == nil || !strings.Contains(err.Error(), "window profile") {
		t.Fatalf("oversized window: %v", err)
	}
}
