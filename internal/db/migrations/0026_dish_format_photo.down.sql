DELETE FROM gap_register WHERE gap_id = 'GAP-029';
DELETE FROM external_source WHERE source_key IN ('BHARAT-INDIAN-FOODS', 'FOODBD');
DROP TABLE IF EXISTS photo_label_archetype_map;
DROP TABLE IF EXISTS dish_format_photo;
