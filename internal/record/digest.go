package record

import (
	"crypto/sha256"
	"encoding/hex"
)

// Digest is the hex SHA-256 of data, the form in which the records and
// the files beside them identify content: environments, configurations,
// indexes, and the names of logs and locks derived from them.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
