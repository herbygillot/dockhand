// Package tart provides shared mechanics for the local Tart installation and
// its images.
//
// Client resolves executable and home paths and runs Tart commands. Image helpers
// provide inventory, manifests, content identities, default names, and cooperative
// locks. The host subpackage controls concrete VMs, provision constructs images,
// and verify/tart owns verification requests and build policy.
package tart
