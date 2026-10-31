package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
)

// CreateAt writes a database at path as an older dockhand left it, at
// schema version, with setup run in it after: for tests of what a newer
// build does with one, a migration it asks a person about among them.
func CreateAt(path string, version int, setup string) error {
	if version < 1 || version > schemaVersion {
		return fmt.Errorf("sqlite: no schema %d", version)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(strings.Join(schemas[:version], "") + fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d;", applicationID, version)); err != nil {
		return err
	}
	_, err = db.Exec(setup)
	return err
}
