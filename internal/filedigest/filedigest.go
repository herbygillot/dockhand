// Package filedigest reads a stream's size and SHA-256 digest.
package filedigest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// Result is the size and lowercase hexadecimal SHA-256 of a stream.
type Result struct {
	Size   int64
	SHA256 string
}

// Sum reads reader to completion and returns its size and SHA-256 digest.
// Cancellation is checked between reads.
func Sum(ctx context.Context, reader io.Reader) (Result, error) {
	hash := sha256.New()
	size, err := io.Copy(hash, contextReader{ctx: ctx, reader: reader})
	if err != nil {
		return Result{}, err
	}
	return Result{Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
