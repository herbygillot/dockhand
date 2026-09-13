ALTER TABLE jobs ADD COLUMN reused_attempt TEXT REFERENCES attempts(id);
ALTER TABLE jobs ADD COLUMN reuse_detail TEXT NOT NULL DEFAULT '';
CREATE INDEX sources_reuse_tree ON sources(repository_id,tree_id,id);
CREATE INDEX attempts_reuse_source ON attempts(repository_id,source_id,json_extract(build,'$.Target.Name'),json_extract(build,'$.Target.Portfile'),created_at DESC,id DESC) WHERE state IN ('finished','canceled');
CREATE INDEX attempts_reuse_target ON attempts(repository_id,json_extract(build,'$.Target.Name'),json_extract(build,'$.Target.Portfile'),created_at DESC,id DESC) WHERE state IN ('finished','canceled');
PRAGMA user_version=5;
