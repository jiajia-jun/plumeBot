package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecallMigrationBackfillsOnlyReliablePrivateOwnership(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "plumebot.db")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"001_initial_schema.sql", "002_group_mgmt.sql", "003_admin_user.sql"} {
		source, err := schemaFS.ReadFile("migrations/" + version)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(source), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("执行旧迁移 %s: %v\nSQL: %s", version, err, statement)
			}
		}
	}
	if _, err := db.Exec(sqlCreateSchemaMigrations); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"001_initial_schema.sql", "002_group_mgmt.sql", "003_admin_user.sql"} {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, version); err != nil {
			t.Fatal(err)
		}
	}
	for _, message := range []struct{ id, groupID, userID, kind string }{
		{"group-old", "g1", "101", "group"},
		{"private-inbound", "", "101", "private"},
		{"self:private-reply", "", "bot", "private"},
	} {
		if _, err := db.Exec(`INSERT INTO messages(message_id, group_id, user_id, parts, timestamp, message_type) VALUES (?, ?, ?, '[]', 10, ?)`,
			message.id, message.groupID, message.userID, message.kind); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(dataDir)
	if err != nil {
		t.Fatalf("应用新召回迁移: %v", err)
	}
	defer store.Close()
	for _, tc := range []struct{ id, want string }{
		{"group-old", "g1"},
		{"private-inbound", "private:101"},
		{"self:private-reply", ""},
	} {
		var got string
		if err := store.db.QueryRow(`SELECT session_key FROM messages WHERE message_id=?`, tc.id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("message %q session_key=%q，期望 %q", tc.id, got, tc.want)
		}
	}
}
