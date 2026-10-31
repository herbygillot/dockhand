-- Q4: clean-configuration differences, not measured in shadow: what
-- mportinit read (Q3), and each setting with the file that set it. A
-- setting whose origin isn't default is one a clean configuration may
-- not share.
SELECT run.mode, s.name, s.value, s.origin
FROM bootstrap_setting s JOIN run ON run.id = s.run_id
ORDER BY s.origin = 'default', s.name;
