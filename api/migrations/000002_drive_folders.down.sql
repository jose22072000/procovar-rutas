-- Only the folders this migration inserted, matched by their folder_id. Anything
-- added from the administration screen stays.
DELETE FROM drive_source WHERE id = md5(folder_id);
