# Warn when the host and the image disagree on MacPorts Base

Asked for on 2026-09-17, after a `bump sslscan` paid for a full PortIndex generation. The shell had `/opt/macports-test/bin` first on PATH, so dockhand evaluated with the test install and generated an index for that install's cache environment, which had no seed. Looking into it raised the question whether the guest follows the host prefix. It does not: provisioning uses the MacPorts package installer, which supports only `/opt/local`, and no host selection reaches the guest. What can differ is the Base release: the host uses whatever `-P`, `MACPORTS_PREFIX`, or PATH selected, the image the release `setup` installed, and nothing said so when they disagreed.

## The warning

Two places, both at info level, neither refusing anything:

- `setup` compares the host Base it inspected with the version it is asked to install or check, and prints, for example, "Warning: the host evaluates ports with MacPorts 2.11.6, but this image builds with MacPorts 2.12.6".
- Build resolution passes the host Base release, read from the evaluation snapshot's runtime, to the Tart provider in its build options. When the image's capabilities have been observed, which is every image that has built at least once, the provider compares and warns, naming the image and both releases and the two ways out: point `-P` or `MACPORTS_PREFIX` at the matching install, or `setup --macports-version` the host's release. An image never observed yet cannot be compared there; setup's warning covers that case.

The host release selects nothing and is not frozen into the provider configuration, so evidence reuse and the configuration digest are unchanged.

## Tests

`verify/tart`: an unobserved image says nothing, an observed 2.12.6 image warns for a 2.11.6 host and stays usable, and a matching host says nothing. `app`: the host release from the snapshot reaches the provider's build options. The tart, app, and cli suites pass.
