CREATE TABLE image_digests (
    provider TEXT NOT NULL,
    path TEXT NOT NULL,
    stamp TEXT NOT NULL,
    digest TEXT NOT NULL,
    PRIMARY KEY (provider, path)
) STRICT;
PRAGMA user_version=7;
