// Package tart provides shared mechanics for the local Tart installation and
// its images.
//
// Client resolves executable and home paths and runs Tart commands. Image helpers
// provide inventory, manifests, content identities, default names, and cooperative
// locks. The host subpackage controls concrete VMs, guestssh reaches a guest,
// provision constructs images, and buildenv/tart owns a check's builds on
// them.
package tart
