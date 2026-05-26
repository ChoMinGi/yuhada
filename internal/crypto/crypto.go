// Package crypto — PII 필드 암복호화 (AES-256-GCM).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

const prefix = "enc:"

// Enc — AES-256-GCM 암호화 핸들. nil이면 암호화 비활성.
type Enc struct {
	key []byte
	gcm cipher.AEAD
}

// New — 32바이트 키로 Enc 생성. 빈 키면 nil 반환 (암호화 없음).
func New(key []byte) (*Enc, error) {
	if len(key) == 0 {
		return nil, nil
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	return &Enc{key: key, gcm: gcm}, nil
}

// Encrypt — 랜덤 nonce AES-GCM. 같은 평문도 매번 다른 암호문.
func (e *Enc) Encrypt(plaintext string) string {
	if e == nil || plaintext == "" {
		return plaintext
	}
	nonce := make([]byte, e.gcm.NonceSize())
	_, _ = rand.Read(nonce)
	ct := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(ct)
}

// EncryptDeterministic — HMAC 파생 nonce. 같은 평문이면 같은 암호문.
// 전화번호처럼 UNIQUE 제약·정확 검색이 필요한 필드에 사용.
func (e *Enc) EncryptDeterministic(plaintext string) string {
	if e == nil || plaintext == "" {
		return plaintext
	}
	h := hmac.New(sha256.New, e.key)
	h.Write([]byte(plaintext))
	nonce := h.Sum(nil)[:e.gcm.NonceSize()]
	ct := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(ct)
}

// Decrypt — "enc:" 접두어가 있으면 복호화, 없으면 평문 그대로 반환 (하위 호환).
func (e *Enc) Decrypt(data string) string {
	if e == nil || !strings.HasPrefix(data, prefix) {
		return data
	}
	raw, err := base64.StdEncoding.DecodeString(data[len(prefix):])
	if err != nil {
		return data
	}
	ns := e.gcm.NonceSize()
	if len(raw) < ns {
		return data
	}
	pt, err := e.gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return data
	}
	return string(pt)
}

// IsEncrypted — "enc:" 접두어 존재 여부.
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, prefix)
}
