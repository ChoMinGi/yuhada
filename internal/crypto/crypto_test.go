package crypto

import (
	"strings"
	"testing"
)

func testKey() []byte {
	return []byte("0123456789abcdef0123456789abcdef") // 32 bytes
}

func TestNew(t *testing.T) {
	t.Run("nil on empty key", func(t *testing.T) {
		enc, err := New(nil)
		if err != nil || enc != nil {
			t.Fatal("expected nil enc, nil err")
		}
	})
	t.Run("error on wrong size", func(t *testing.T) {
		_, err := New([]byte("short"))
		if err == nil {
			t.Fatal("expected error for short key")
		}
	})
	t.Run("success on 32 bytes", func(t *testing.T) {
		enc, err := New(testKey())
		if err != nil || enc == nil {
			t.Fatal("expected valid enc")
		}
	})
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	enc, _ := New(testKey())

	cases := []string{
		"hello",
		"김지영",
		"01012345678",
		"특수문자!@#$%",
		"긴 문장 테스트: 유하다 헤어 정액권 관리 시스템",
	}
	for _, tc := range cases {
		ct := enc.Encrypt(tc)
		if ct == tc {
			t.Errorf("Encrypt(%q) should differ from plaintext", tc)
		}
		if !strings.HasPrefix(ct, prefix) {
			t.Errorf("Encrypt(%q) should start with %q", tc, prefix)
		}
		pt := enc.Decrypt(ct)
		if pt != tc {
			t.Errorf("Decrypt(Encrypt(%q)) = %q, want %q", tc, pt, tc)
		}
	}
}

func TestEncryptRandomNonce(t *testing.T) {
	enc, _ := New(testKey())
	a := enc.Encrypt("same")
	b := enc.Encrypt("same")
	if a == b {
		t.Error("Encrypt should produce different ciphertext each time")
	}
}

func TestEncryptDeterministic(t *testing.T) {
	enc, _ := New(testKey())
	a := enc.EncryptDeterministic("01012345678")
	b := enc.EncryptDeterministic("01012345678")
	if a != b {
		t.Error("EncryptDeterministic should produce same ciphertext for same input")
	}
	// different input → different ciphertext
	c := enc.EncryptDeterministic("01099998888")
	if a == c {
		t.Error("different inputs should produce different ciphertext")
	}
	// round-trip
	pt := enc.Decrypt(a)
	if pt != "01012345678" {
		t.Errorf("Decrypt(EncryptDeterministic) = %q, want 01012345678", pt)
	}
}

func TestDecryptPlaintextPassthrough(t *testing.T) {
	enc, _ := New(testKey())
	// no "enc:" prefix → return as-is
	if enc.Decrypt("plain text") != "plain text" {
		t.Error("should pass through plaintext without enc: prefix")
	}
}

func TestNilEncNoOp(t *testing.T) {
	var enc *Enc
	if enc.Encrypt("hello") != "hello" {
		t.Error("nil Enc.Encrypt should return plaintext")
	}
	if enc.EncryptDeterministic("hello") != "hello" {
		t.Error("nil Enc.EncryptDeterministic should return plaintext")
	}
	if enc.Decrypt("hello") != "hello" {
		t.Error("nil Enc.Decrypt should return plaintext")
	}
}

func TestEncryptEmpty(t *testing.T) {
	enc, _ := New(testKey())
	if enc.Encrypt("") != "" {
		t.Error("empty string should remain empty")
	}
	if enc.EncryptDeterministic("") != "" {
		t.Error("empty string should remain empty")
	}
}

func TestDecryptCorruptedCiphertext(t *testing.T) {
	enc, _ := New(testKey())
	// invalid base64
	if enc.Decrypt("enc:!!!invalid!!!") != "enc:!!!invalid!!!" {
		t.Error("corrupted base64 should return as-is")
	}
	// valid base64 but garbage content
	if enc.Decrypt("enc:AAAA") != "enc:AAAA" {
		t.Error("short ciphertext should return as-is")
	}
	// tampered ciphertext
	ct := enc.Encrypt("hello")
	tampered := ct[:len(ct)-2] + "XX"
	if enc.Decrypt(tampered) != tampered {
		t.Error("tampered ciphertext should return as-is")
	}
}

func TestIsEncrypted(t *testing.T) {
	if IsEncrypted("plain") {
		t.Error("plain should not be encrypted")
	}
	if !IsEncrypted("enc:abc123") {
		t.Error("enc: prefix should be detected")
	}
}
