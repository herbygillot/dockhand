package record

import "github.com/herbygillot/dockhand/internal/model"

// Digest is the hex SHA-256 of data; see model.Digest.
func Digest(data []byte) string { return model.Digest(data) }
