ALTER TABLE changes ADD COLUMN initiating_target TEXT NOT NULL DEFAULT '';
UPDATE changes SET initiating_target=json_extract(targets,'$[0].Name') WHERE json_array_length(targets)=1;
CREATE INDEX changes_target ON changes(repository_id,initiating_target COLLATE NOCASE,disposition,id);
ALTER TABLE requests ADD COLUMN joined_job TEXT REFERENCES jobs(id);
INSERT INTO changes(id,repository_id,branch,disposition,targets,created_at,initiating_target)
 SELECT 'change_'||id,repository_id,'',CASE WHEN json_extract(resolved_release,'$.NoUpdate')=1 THEN 'closed' ELSE 'open' END,
 json_extract(options,'$.Targets'),accepted_at,json_extract(options,'$.Targets[0].Name')
 FROM jobs WHERE change_id IS NULL AND action IN ('bump','bump-revision','refresh-checksums')
 AND json_array_length(options,'$.Targets')=1 AND json_type(options,'$.Preparation')='object';
UPDATE jobs SET change_id='change_'||id WHERE change_id IS NULL AND action IN ('bump','bump-revision','refresh-checksums')
 AND json_array_length(options,'$.Targets')=1 AND json_type(options,'$.Preparation')='object';
