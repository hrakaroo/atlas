-- Copied from app/library.py Library.migrate_relationships; preserve compatibility.
CREATE TABLE IF NOT EXISTS entities (
    id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('person','team','project')),
    name TEXT NOT NULL, profile_id TEXT REFERENCES items(id), legacy_tag_id TEXT UNIQUE,
    UNIQUE(kind,name));
CREATE TABLE IF NOT EXISTS entity_relationships (
    item_id TEXT NOT NULL REFERENCES items(id), entity_id TEXT NOT NULL REFERENCES entities(id),
    relation TEXT NOT NULL, PRIMARY KEY(item_id,entity_id));
INSERT OR IGNORE INTO entities(id,kind,name,legacy_tag_id)
    SELECT id,category,name,id FROM tags WHERE category IN ('person','team','project');
UPDATE entities SET profile_id=(SELECT i.id FROM items i WHERE i.kind=entities.kind
    AND (lower(i.title)=lower(entities.name) OR lower(json_extract(i.metadata,'$.obsidian_stem'))=lower(entities.name))
    ORDER BY i.deleted_at IS NOT NULL,i.created LIMIT 1) WHERE profile_id IS NULL;
INSERT OR IGNORE INTO entity_relationships
    SELECT it.item_id,e.id,
        CASE WHEN e.kind='team' AND i.kind='person' THEN 'member_of'
             WHEN e.kind='project' AND i.kind='person' THEN 'works_on'
             WHEN e.kind='project' THEN 'belongs_to' ELSE 'related_to' END
    FROM item_tags it JOIN entities e ON e.legacy_tag_id=it.tag_id JOIN items i ON i.id=it.item_id
    WHERE NOT (e.kind=i.kind AND (lower(e.name)=lower(i.title)
        OR lower(e.name)=lower(COALESCE(json_extract(i.metadata,'$.obsidian_stem'),''))));
CREATE TRIGGER IF NOT EXISTS entity_legacy_create AFTER INSERT ON tags
WHEN NEW.category IN ('person','team','project') BEGIN
    INSERT OR IGNORE INTO entities(id,kind,name,legacy_tag_id,profile_id)
        VALUES(NEW.id,NEW.category,NEW.name,NEW.id,
            (SELECT id FROM items WHERE kind=NEW.category AND (lower(title)=lower(NEW.name)
            OR lower(json_extract(metadata,'$.obsidian_stem'))=lower(NEW.name))
            ORDER BY deleted_at IS NOT NULL,created LIMIT 1));
END;
CREATE TRIGGER IF NOT EXISTS entity_profile_create AFTER INSERT ON items
WHEN NEW.kind IN ('person','team','project') BEGIN
    UPDATE entities SET profile_id=NEW.id WHERE profile_id IS NULL AND kind=NEW.kind
        AND (lower(name)=lower(NEW.title) OR lower(name)=lower(json_extract(NEW.metadata,'$.obsidian_stem')));
END;
CREATE TRIGGER IF NOT EXISTS entity_legacy_link AFTER INSERT ON item_tags BEGIN
    INSERT OR IGNORE INTO entity_relationships
        SELECT NEW.item_id,e.id,
            CASE WHEN e.kind='team' AND i.kind='person' THEN 'member_of'
                 WHEN e.kind='project' AND i.kind='person' THEN 'works_on'
                 WHEN e.kind='project' THEN 'belongs_to' ELSE 'related_to' END
        FROM entities e JOIN items i ON i.id=NEW.item_id WHERE e.legacy_tag_id=NEW.tag_id
            AND NOT(e.kind=i.kind AND (lower(e.name)=lower(i.title)
            OR lower(e.name)=lower(COALESCE(json_extract(i.metadata,'$.obsidian_stem'),''))));
END;
CREATE TRIGGER IF NOT EXISTS entity_legacy_unlink AFTER DELETE ON item_tags BEGIN
    DELETE FROM entity_relationships WHERE item_id=OLD.item_id
        AND entity_id IN (SELECT id FROM entities WHERE legacy_tag_id=OLD.tag_id);
END;
