package nspkt

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

const r2cryptoNonceSize = 12
const r2cryptoTagSize = 16

var r2cryptoGCM cipher.AEAD
var r2cryptoKey = []byte("X3V.bXCfe3EhN'wb")
var r2cryptoAAD = []byte("\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10")

// r2cb efficiently implements allocation-free Titanfall 2 packet crypto.
//
//	go:            data tag
//	net: nonce tag data
type r2cb []byte

// init initializes the AES-GCM cipher for Titanfall 2 packet crypto.
func init() {
	if c, err := aes.NewCipher([]byte(r2cryptoKey)); err != nil {
		panic(fmt.Errorf("r2crypto: init aes: %w", err))
	} else if a, err := cipher.NewGCMWithTagSize(c, r2cryptoTagSize); err != nil {
		panic(fmt.Errorf("r2crypto: init gcm: %w", err))
	} else if n := a.NonceSize(); n != r2cryptoNonceSize {
		panic(fmt.Errorf("r2crypto: unexpected nonce size %d", n))
	} else {
		r2cryptoGCM = a
	}
}

// r2crypto allocates a new buffer which can hold up to n bytes of data.
func r2crypto(n int) r2cb {
	return make(r2cb, r2cryptoNonceSize+r2cryptoTagSize+n+r2cryptoTagSize)
}

// WithPacketLen returns a slice of the buffer for a packet of length n.
func (pkt r2cb) WithPacketLen(n int) r2cb {
	return pkt[:n+r2cryptoTagSize]
}

// WithDataLen returns a slice of the buffer for a packet with data of length n.
func (pkt r2cb) WithDataLen(n int) r2cb {
	return pkt[:r2cryptoNonceSize+r2cryptoTagSize+n+r2cryptoTagSize]
}

// Packet returns a slice of the buffer containing the raw packet.
func (pkt r2cb) Packet() []byte {
	return pkt[:len(pkt)-r2cryptoTagSize]
}

// Data returns a slice of the buffer contains the packet data.
func (pkt r2cb) Data() []byte {
	return pkt[r2cryptoNonceSize+r2cryptoTagSize : len(pkt)-r2cryptoTagSize]
}

// Nonce returns a slice of the buffer containing the nonce. It should be
// randomized before calling Encrypt.
func (pkt r2cb) Nonce() []byte {
	return pkt[:r2cryptoNonceSize]
}

func (pkt r2cb) tagNet() []byte {
	return pkt[r2cryptoNonceSize:][:r2cryptoTagSize]
}

func (pkt r2cb) tagGo() []byte {
	return pkt[len(pkt)-r2cryptoTagSize:][:r2cryptoTagSize]
}

func (pkt r2cb) gcmGo() []byte {
	return pkt[r2cryptoNonceSize+r2cryptoTagSize:]
}

// Decrypt decrypts the packet data in-place. It is the inverse of Encrypt.
func (pkt r2cb) Decrypt() bool {
	copy(pkt.tagGo(), pkt.tagNet())
	b, err := r2cryptoGCM.Open(pkt.Data()[:0], pkt.Nonce(), pkt.gcmGo(), r2cryptoAAD)
	if len(b) != 0 && len(pkt.Data()) != 0 && &b[0] != &pkt.Data()[0] {
		panic("buffer was moved (wtf?)")
	}
	return err == nil
}

// Encrypt encrypts the packet data in-place. It is the inverse of Decrypt.
func (pkt r2cb) Encrypt() {
	b := r2cryptoGCM.Seal(pkt.gcmGo()[:0], pkt.Nonce(), pkt.Data(), r2cryptoAAD)
	if len(b) != 0 && len(pkt.Data()) != 0 && &b[0] != &pkt.Data()[0] {
		panic("buffer was moved (wtf?)")
	}
	copy(pkt.tagNet(), pkt.tagGo())
}
