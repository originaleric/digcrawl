package wechatchannels

import (
	"encoding/binary"
	"io"
)

// channelsDecryptReader reproduces the documented Channels media-prefix
// transform. Only the first encryptedLimit bytes are transformed; the rest of
// the response is copied exactly as delivered by the media CDN.
type channelsDecryptReader struct {
	reader    io.Reader
	cipher    channelsISAAC
	remaining uint64
	block     [8]byte
	blockPos  int
}

func newChannelsDecryptReader(reader io.Reader, key uint64, encryptedLimit uint64) *channelsDecryptReader {
	return &channelsDecryptReader{reader: reader, cipher: newChannelsISAAC(key), remaining: encryptedLimit, blockPos: 8}
}

func (reader *channelsDecryptReader) Read(buffer []byte) (int, error) {
	n, err := reader.reader.Read(buffer)
	if n == 0 || reader.remaining == 0 {
		return n, err
	}
	count := uint64(n)
	if count > reader.remaining {
		count = reader.remaining
	}
	for index := uint64(0); index < count; index++ {
		if reader.blockPos == len(reader.block) {
			binary.BigEndian.PutUint64(reader.block[:], reader.cipher.next())
			reader.blockPos = 0
		}
		buffer[index] ^= reader.block[reader.blockPos]
		reader.blockPos++
	}
	reader.remaining -= count
	return n, err
}

// channelsISAAC is the 64-bit ISAAC keystream used by encrypted Channels
// media. It intentionally has no other callers or persistence surface.
type channelsISAAC struct {
	seed    [256]uint64
	memory  [256]uint64
	a, b, c uint64
	index   uint64
}

func newChannelsISAAC(key uint64) channelsISAAC {
	cipher := channelsISAAC{index: 255}
	cipher.seed[0] = key
	var a, b, c, d, e, f, g, h = uint64(0x9e3779b97f4a7c13), uint64(0x9e3779b97f4a7c13), uint64(0x9e3779b97f4a7c13), uint64(0x9e3779b97f4a7c13), uint64(0x9e3779b97f4a7c13), uint64(0x9e3779b97f4a7c13), uint64(0x9e3779b97f4a7c13), uint64(0x9e3779b97f4a7c13)
	for round := 0; round < 4; round++ {
		channelsISAACMix(&a, &b, &c, &d, &e, &f, &g, &h)
	}
	for offset := 0; offset < 256; offset += 8 {
		a += cipher.seed[offset]
		b += cipher.seed[offset+1]
		c += cipher.seed[offset+2]
		d += cipher.seed[offset+3]
		e += cipher.seed[offset+4]
		f += cipher.seed[offset+5]
		g += cipher.seed[offset+6]
		h += cipher.seed[offset+7]
		channelsISAACMix(&a, &b, &c, &d, &e, &f, &g, &h)
		cipher.memory[offset], cipher.memory[offset+1], cipher.memory[offset+2], cipher.memory[offset+3] = a, b, c, d
		cipher.memory[offset+4], cipher.memory[offset+5], cipher.memory[offset+6], cipher.memory[offset+7] = e, f, g, h
	}
	for offset := 0; offset < 256; offset += 8 {
		a += cipher.memory[offset]
		b += cipher.memory[offset+1]
		c += cipher.memory[offset+2]
		d += cipher.memory[offset+3]
		e += cipher.memory[offset+4]
		f += cipher.memory[offset+5]
		g += cipher.memory[offset+6]
		h += cipher.memory[offset+7]
		channelsISAACMix(&a, &b, &c, &d, &e, &f, &g, &h)
		cipher.memory[offset], cipher.memory[offset+1], cipher.memory[offset+2], cipher.memory[offset+3] = a, b, c, d
		cipher.memory[offset+4], cipher.memory[offset+5], cipher.memory[offset+6], cipher.memory[offset+7] = e, f, g, h
	}
	cipher.generate()
	return cipher
}

func (cipher *channelsISAAC) next() uint64 {
	value := cipher.seed[cipher.index]
	if cipher.index == 0 {
		cipher.generate()
		cipher.index = 255
	} else {
		cipher.index--
	}
	return value
}

func (cipher *channelsISAAC) generate() {
	cipher.c++
	cipher.b += cipher.c
	for index := 0; index < 256; index++ {
		switch index % 4 {
		case 0:
			cipher.a = ^(cipher.a ^ (cipher.a << 21))
		case 1:
			cipher.a ^= cipher.a >> 5
		case 2:
			cipher.a ^= cipher.a << 12
		case 3:
			cipher.a ^= cipher.a >> 33
		}
		cipher.a += cipher.memory[(index+128)%256]
		x := cipher.memory[index]
		y := cipher.memory[(x>>3)%256] + cipher.a + cipher.b
		cipher.memory[index] = y
		cipher.b = cipher.memory[(y>>11)%256] + x
		cipher.seed[index] = cipher.b
	}
}

func channelsISAACMix(a, b, c, d, e, f, g, h *uint64) {
	*a -= *e
	*f ^= *h >> 9
	*h += *a
	*b -= *f
	*g ^= *a << 9
	*a += *b
	*c -= *g
	*h ^= *b >> 23
	*b += *c
	*d -= *h
	*a ^= *c << 15
	*c += *d
	*e -= *a
	*b ^= *d >> 14
	*d += *e
	*f -= *b
	*c ^= *e << 20
	*e += *f
	*g -= *c
	*d ^= *f >> 17
	*f += *g
	*h -= *d
	*e ^= *g << 14
	*g += *h
}
