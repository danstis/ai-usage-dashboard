// Package secret implements the credential crypto core for P2/S2: sealing
// and opening credential secrets with AES-256-GCM. It has no storage and no
// API — it is a pure crypto primitive consumed by the credential store (S3)
// and the fetch/scheduler path. Open must never be wired into an HTTP read
// path; only write-time seal and fetch-time open are legitimate callers.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize is the required AES-256 key length in bytes.
const KeySize = 32

// NonceSize is the GCM nonce length in bytes, drawn fresh from crypto/rand
// for every Seal call.
const NonceSize = 12

// version identifies the blob layout so a future scheme or key rotation can
// be introduced unambiguously. version 1 is: version(1 byte) ‖ nonce(12
// bytes) ‖ ciphertext+tag.
const version byte = 0x01

// aadDomain domain-separates credential AAD from any other future use of
// this package, so a blob sealed for one purpose can never be opened as if
// it were sealed for another.
const aadDomain = "aud/cred/v1"

// Typed errors. None of these, nor any error returned by this package, ever
// include plaintext or key material — only fixed, static messages.
var (
	// ErrInvalidKeyLength is returned when key is not exactly KeySize bytes.
	ErrInvalidKeyLength = errors.New("secret: invalid key length")
	// ErrInvalidBlob is returned when a blob is too short or carries an
	// unrecognized version byte.
	ErrInvalidBlob = errors.New("secret: invalid blob")
	// ErrDecryptionFailed is returned when authenticated decryption fails —
	// wrong key, wrong AAD, or a tampered/corrupted blob. GCM's design does
	// not distinguish these cases, and neither does this package.
	ErrDecryptionFailed = errors.New("secret: decryption failed")
)

// AAD builds the domain-separated additional authenticated data that binds
// a sealed credential to the exact provider and field it belongs to:
// "aud/cred/v1" ‖ 0x00 ‖ providerID ‖ 0x00 ‖ fieldName. The NUL separators
// prevent the concatenation ambiguity a plain join would have (providerID
// "a" + fieldName "bc" would otherwise be indistinguishable from providerID
// "ab" + fieldName "c").
func AAD(providerID, fieldName string) []byte {
	aad := make([]byte, 0, len(aadDomain)+1+len(providerID)+1+len(fieldName))
	aad = append(aad, aadDomain...)
	aad = append(aad, 0x00)
	aad = append(aad, providerID...)
	aad = append(aad, 0x00)
	aad = append(aad, fieldName...)
	return aad
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeyLength
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: new gcm: %w", err)
	}
	return gcm, nil
}

// Seal encrypts plaintext under key, authenticating it (and binding it to)
// aad, and returns the resulting blob: version(1) ‖ nonce(12) ‖
// ciphertext+tag. A fresh random nonce is drawn from crypto/rand for every
// call, so sealing the same plaintext twice yields different blobs. key
// must be exactly KeySize bytes.
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret: generate nonce: %w", err)
	}

	blob := make([]byte, 0, 1+NonceSize+len(plaintext)+gcm.Overhead())
	blob = append(blob, version)
	blob = append(blob, nonce...)
	blob = gcm.Seal(blob, nonce, plaintext, aad)
	return blob, nil
}

// Open authenticates and decrypts blob under key and aad, returning the
// original plaintext. It returns ErrInvalidKeyLength if key is not KeySize
// bytes, ErrInvalidBlob if blob is too short or has an unrecognized version
// byte, or ErrDecryptionFailed if authentication fails — which covers a
// wrong key, a wrong aad, and a tampered or corrupted blob alike.
func Open(key, blob, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	if len(blob) < 1+NonceSize {
		return nil, ErrInvalidBlob
	}
	if blob[0] != version {
		return nil, ErrInvalidBlob
	}

	nonce := blob[1 : 1+NonceSize]
	ciphertext := blob[1+NonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}
