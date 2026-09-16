// Package credential defines secret-storage and device-authorization contracts.
//
// Device flows present short-lived authorization prompts and return credentials;
// stores persist secrets under service and account keys. Implementations supply
// the authentication protocol and storage mechanism, while callers own prompts
// and the decision to save or remove a credential.
package credential
