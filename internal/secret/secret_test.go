package secret

import (
	"bytes"
	"errors"
	"testing"
)

func validKey() []byte {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func otherKey() []byte {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(255 - i)
	}
	return key
}

func TestSealOpen_RoundTrip(t *testing.T) {
	key := validKey()
	plaintext := []byte("super-secret-api-key")
	aad := AAD("openai", "api_key")

	blob, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}

	got, err := Open(key, blob, aad)
	if err != nil {
		t.Fatalf("Open: unexpected error: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Open: got %q, want %q", got, plaintext)
	}
}

func TestOpen_TamperedCiphertext(t *testing.T) {
	key := validKey()
	aad := AAD("openai", "api_key")

	blob, err := Seal(key, []byte("super-secret-api-key"), aad)
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}

	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := Open(key, tampered, aad); err == nil {
		t.Fatal("Open: expected error for tampered ciphertext, got nil")
	}
}

func TestOpen_WrongKey(t *testing.T) {
	aad := AAD("openai", "api_key")

	blob, err := Seal(validKey(), []byte("super-secret-api-key"), aad)
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}

	if _, err := Open(otherKey(), blob, aad); err == nil {
		t.Fatal("Open: expected error for wrong key, got nil")
	}
}

func TestOpen_WrongAAD(t *testing.T) {
	key := validKey()

	blob, err := Seal(key, []byte("super-secret-api-key"), AAD("openai", "api_key"))
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}

	if _, err := Open(key, blob, AAD("anthropic", "api_key")); err == nil {
		t.Fatal("Open: expected error for wrong AAD, got nil")
	}
}

func TestSeal_NonceUniqueness(t *testing.T) {
	key := validKey()
	plaintext := []byte("super-secret-api-key")
	aad := AAD("openai", "api_key")

	blob1, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}
	blob2, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}

	if bytes.Equal(blob1, blob2) {
		t.Fatal("Seal: two seals of the same plaintext produced identical blobs (nonce reuse)")
	}

	// The nonce lives right after the version byte; it must differ between
	// calls even though everything else about the inputs is identical.
	nonce1 := blob1[1 : 1+NonceSize]
	nonce2 := blob2[1 : 1+NonceSize]
	if bytes.Equal(nonce1, nonce2) {
		t.Fatal("Seal: nonce was reused across two calls")
	}
}

func TestSeal_WrongKeyLength(t *testing.T) {
	shortKey := make([]byte, 16)
	if _, err := Seal(shortKey, []byte("plaintext"), AAD("openai", "api_key")); err == nil {
		t.Fatal("Seal: expected error for wrong key length, got nil")
	} else if !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("Seal: expected ErrInvalidKeyLength, got %v", err)
	}
}

func TestOpen_WrongKeyLength(t *testing.T) {
	key := validKey()
	blob, err := Seal(key, []byte("plaintext"), AAD("openai", "api_key"))
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}

	shortKey := make([]byte, 16)
	if _, err := Open(shortKey, blob, AAD("openai", "api_key")); err == nil {
		t.Fatal("Open: expected error for wrong key length, got nil")
	} else if !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("Open: expected ErrInvalidKeyLength, got %v", err)
	}
}

// TestAAD_SeparatorAmbiguity proves the domain-separated AAD format removes
// the concatenation ambiguity a naive providerID+fieldName join would have:
// AAD("a", "bc") and AAD("ab", "c") must not collide.
func TestAAD_SeparatorAmbiguity(t *testing.T) {
	a := AAD("a", "bc")
	b := AAD("ab", "c")

	if bytes.Equal(a, b) {
		t.Fatal("AAD: AAD(\"a\",\"bc\") and AAD(\"ab\",\"c\") must not collide")
	}
}

func TestOpen_AADSeparatorAmbiguity_CrossFieldFails(t *testing.T) {
	key := validKey()
	plaintext := []byte("super-secret-api-key")

	blob, err := Seal(key, plaintext, AAD("a", "bc"))
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}

	if _, err := Open(key, blob, AAD("ab", "c")); err == nil {
		t.Fatal("Open: expected error opening with the AAD for a different provider/field split, got nil")
	}
}

func TestOpen_TruncatedBlob(t *testing.T) {
	key := validKey()
	if _, err := Open(key, []byte{0x01, 0x02, 0x03}, AAD("openai", "api_key")); err == nil {
		t.Fatal("Open: expected error for truncated blob, got nil")
	}
}

func TestOpen_UnknownVersion(t *testing.T) {
	key := validKey()
	aad := AAD("openai", "api_key")

	blob, err := Seal(key, []byte("plaintext"), aad)
	if err != nil {
		t.Fatalf("Seal: unexpected error: %v", err)
	}

	corrupted := bytes.Clone(blob)
	corrupted[0] = 0xFF

	if _, err := Open(key, corrupted, aad); err == nil {
		t.Fatal("Open: expected error for unknown version byte, got nil")
	}
}
