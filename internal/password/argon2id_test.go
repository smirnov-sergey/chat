package password_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"chat/internal/password"
	"chat/internal/service"

	"golang.org/x/crypto/argon2"
)

var _ service.PasswordHasher = password.Argon2id{}

func TestArgon2idHash(t *testing.T) {
	const original = "  My Passphrase ПРИВЕТ 😀  "
	hasher := password.Argon2id{}
	encoded, err := hasher.Hash(original)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		t.Fatal("expected an Argon2id PHC string with version 19")
	}
	var memory, iterations uint32
	var parallelism uint8
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil || n != 3 {
		t.Fatal("hash must include its memory, iteration and parallelism parameters")
	}
	if memory != 19*1024 || iterations != 2 || parallelism != 1 {
		t.Fatal("expected the selected OWASP minimum Argon2id configuration")
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		t.Fatal("expected a 16-byte salt encoded as unpadded standard base64")
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) != 32 {
		t.Fatal("expected a 32-byte key encoded as unpadded standard base64")
	}

	// Recompute using only the password and the information persisted in the string.
	expected := argon2.IDKey([]byte(original), salt, iterations, memory, parallelism, uint32(len(key)))
	if !bytes.Equal(key, expected) {
		t.Fatal("stored parameters and salt must reproduce the key for the unchanged password")
	}
	other, err := hasher.Hash(original)
	if err != nil {
		t.Fatal(err)
	}
	otherParts := strings.Split(other, "$")
	if len(otherParts) != 6 || otherParts[4] == parts[4] || otherParts[5] == parts[5] {
		t.Fatal("repeated hashing of the same password must use a new salt and produce a different key")
	}
}
