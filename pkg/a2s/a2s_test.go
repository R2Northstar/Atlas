package a2s

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestPacketRoundTrip(t *testing.T) {
	b := r2encodeGetChallenge(1000000001337)
	e := mustDecodeHex("ffffffff48636f6e6e656374003915a5d4e800000002")

	if !bytes.Equal(b, e) {
		t.Error("incorrect getchallenge encoding")
	}

	be, err := r2cryptoEncrypt(b)
	if err != nil {
		t.Errorf("failed to encrypt packet: %v", err)
	}

	bd, err := r2cryptoDecrypt(be)
	if err != nil {
		t.Errorf("failed to decrypt packet: %v", err)
	}

	if !bytes.Equal(bd, b) {
		t.Error("incorrect decryption result")
	}
}

func TestDecodeChallenge(t *testing.T) {
	b := mustDecodeHex("f4ca55b7f53a2f9c19b563010d6964869648a23be1db9edce9f55ee3f9a02451be86ba56447740d1d893c34f3a854f6efbd47605ebf3211e05")

	bd, err := r2cryptoDecrypt(b)
	if err != nil {
		t.Errorf("failed to decrypt packet: %v", err)
	}

	uid, challenge, err := r2decodeChallenge(bd)
	if err != nil {
		t.Errorf("failed to parse packet: %v", err)
	}

	if uid != 1000000001337 {
		t.Errorf("incorrect uid")
	}

	if challenge != 81930672 {
		t.Errorf("incorrect challenge")
	}
}
func FuzzGetChallenge(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(1000000001337))

	f.Fuzz(func(t *testing.T, uid uint64) {
		b := r2encodeGetChallenge(uid)

		be, err := r2cryptoEncrypt(b)
		if err != nil {
			t.Errorf("failed to encrypt packet: %v", err)
		}

		bd, err := r2cryptoDecrypt(be)
		if err != nil {
			t.Errorf("failed to decrypt packet: %v", err)
		}

		if !bytes.Equal(bd, b) {
			t.Error("incorrect decryption result")
		}
	})
}

func FuzzChallenge(f *testing.F) {
	f.Add(mustDecodeHex("aa"))
	f.Add(mustDecodeHex("aaaaaaaaaaaaaa"))
	f.Add(mustDecodeHex("00000000000000"))
	f.Add(mustDecodeHex("09f7b6c1f41d91ecb41f370e9fd085610e5ee98827ba7aa9789557e18ddb2a28587f635a008aa71b9cb7b3f38b3ccd8d1ff0"))
	f.Add(mustDecodeHex("edf3552e5d364fb3ab5505822c45c107208251b836022ad94698d920cfec348c469a861d14b5af2d8ca12702d09a7d91796e"))
	f.Add(mustDecodeHex("f4ca55b7f53a2f9c19b563010d6964869648a23be1db9edce9f55ee3f9a02451be86ba56447740d1d893c34f3a854f6efbd47605ebf3211e05"))
	f.Add(mustDecodeHex("bb8aaeed936b6dea21ba8bf4db5ca22a823a122307d5c6bc4124994581eb07b7996575acbbafe28ea4aee8bb58c681e33528470900007b012a"))

	f.Fuzz(func(_ *testing.T, pkt []byte) {
		// ensure this doesn't panic
		r2cryptoDecrypt(pkt)
	})
}

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Errorf("decode %q: %w", s, err))
	}
	return b
}
