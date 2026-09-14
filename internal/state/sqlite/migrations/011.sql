CREATE TABLE image_capabilities (
    provider TEXT NOT NULL,
    environment_digest TEXT NOT NULL,
    capability_digest TEXT NOT NULL,
    capabilities TEXT NOT NULL CHECK(json_valid(capabilities)),
    problem TEXT NOT NULL,
    observed_at INTEGER NOT NULL,
    PRIMARY KEY (provider, environment_digest)
) STRICT;
PRAGMA user_version=11;
