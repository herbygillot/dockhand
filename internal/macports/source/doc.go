// Package source interprets the upstream conventions of evaluated Portfiles.
//
// It maps supported GitHub and GitLab PortGroup options to repository identity,
// tag patterns, catalog preference, and livecheck matching rules. Its candidate
// match text models PortGroup conventions for selection. Forge observations and
// actual source-archive transfers belong to callers.
package source
