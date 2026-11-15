package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	memory      uint32 = 19 * 1024 // KiB
	iterations  uint32 = 2
	parallelism uint8  = 1
	saltLength         = 16
	keyLength   uint32 = 32
)

type Argon2id struct{}

var ErrInvalidHash = errors.New("invalid or unsupported password hash")

func (Argon2id) Hash(password string) (string, error) {
	salt := make([]byte, saltLength)
	// crypto/rand.Read always fills the buffer and never returns an error.
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)

	return fmt.Sprintf("%s%s$%s",
		argon2idPrefix(),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func (Argon2id) Verify(password, encoded string) (bool, error) {
	salt, storedKey, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}

	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	return subtle.ConstantTimeCompare(key, storedKey) == 1, nil
}

func decodeHash(encoded string) (salt, key []byte, err error) {
	prefix := argon2idPrefix()
	expectedLength := len(prefix) + base64.RawStdEncoding.EncodedLen(saltLength) + 1 + base64.RawStdEncoding.EncodedLen(int(keyLength))
	if len(encoded) != expectedLength {
		return nil, nil, ErrInvalidHash
	}

	body, ok := strings.CutPrefix(encoded, prefix)
	if !ok {
		return nil, nil, ErrInvalidHash
	}

	encodedSalt, encodedKey, ok := strings.Cut(body, "$")
	if !ok {
		return nil, nil, ErrInvalidHash
	}

	salt, err = base64.RawStdEncoding.Strict().DecodeString(encodedSalt)
	if err != nil || len(salt) != saltLength {
		return nil, nil, ErrInvalidHash
	}

	key, err = base64.RawStdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) != int(keyLength) {
		return nil, nil, ErrInvalidHash
	}

	return salt, key, nil
}

func argon2idPrefix() string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$", argon2.Version, memory, iterations, parallelism)
}
