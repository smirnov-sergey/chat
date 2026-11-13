package password

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

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

func (Argon2id) Hash(password string) (string, error) {
	salt := make([]byte, saltLength)
	// crypto/rand.Read always fills the buffer and never returns an error.
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		memory,
		iterations,
		parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}
