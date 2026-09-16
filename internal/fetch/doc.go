// Package fetch performs bounded HTTP transfers using caller-owned requests and
// clients.
//
// Open checks response status and redirect safety and enforces the supplied body
// size limit. The request context controls cancellation. Callers close the body
// and own content validation, hashing, storage, and cache policy.
package fetch
