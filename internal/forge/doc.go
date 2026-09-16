// Package forge defines remote repository observations and publication inputs
// shared by forge adapters and their consumers.
//
// Its contracts describe tags, releases, repository identity, pull requests, and
// remote failures. Adapters translate service responses into these values;
// upstream and publish apply port-selection and publication policy.
package forge
