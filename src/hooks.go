package main


/*
CREATE FUNCTION attach_association() RETURNS TRIGGER AS $attach_association$
BEGIN
    UPDATE messages
    SET associations = ARRAY_APPEND(associations, NEW.id)
    WHERE id = NEW.target;
    return NEW;
END;
$attach_association$
LANGUAGE plpgsql;
CREATE TRIGGER attach_association_trigger
    AFTER INSERT
    ON associations
    FOR EACH ROW EXECUTE FUNCTION attach_association();
CREATE FUNCTION detach_association() RETURNS TRIGGER AS $detach_association$
BEGIN
    UPDATE messages
    SET associations = ARRAY_REMOVE(associations, OLD.id)
    WHERE id = NEW.target;
    return OLD;
END;
$detach_association$
LANGUAGE plpgsql;
CREATE TRIGGER detach_association_trigger
    BEFORE DELETE 
    ON associations
    FOR EACH ROW EXECUTE FUNCTION detach_association();
*/

