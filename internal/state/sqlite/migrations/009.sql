ALTER TABLE jobs ADD COLUMN phase TEXT NOT NULL DEFAULT 'verification'
 CHECK(phase IN ('preparation','verification','publication'));
UPDATE jobs SET phase=CASE
 WHEN action='publish' THEN 'publication'
 WHEN action IN ('bump','bump-revision','refresh-checksums') AND (destination='branch-ready' OR result_revision IS NULL) THEN 'preparation'
 WHEN action IN ('bump','bump-revision') AND destination='published' AND (
  reused_attempt IS NOT NULL OR
  EXISTS(SELECT 1 FROM publications p WHERE p.repository_id=jobs.repository_id AND p.job_id=jobs.id) OR
  EXISTS(SELECT 1 FROM attempts a JOIN attempt_evidence e ON e.repository_id=a.repository_id AND e.attempt_id=a.id
   WHERE a.repository_id=jobs.repository_id AND a.job_id=jobs.id AND a.state='finished' AND json_extract(e.evidence,'$.Verdict')='passed')
 ) THEN 'publication'
 ELSE 'verification'
END;
PRAGMA user_version=9;
