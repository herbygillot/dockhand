// Package sqlite implements Dockhand's state contracts using SQLite.
//
// One database stores repository-scoped workflows together with shared provider
// coordination and image observations. The backend owns schema migrations,
// transaction deadlines, locking, integrity checks, and database maintenance.
// It enforces durable identities, relationships, and lifecycle checkpoints while
// leaving workflow eligibility and policy to callers. Read-only opens support
// observation without initializing missing state.
package sqlite
