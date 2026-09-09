package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenNormalizesLegacyDuplicateQueuedPositionsBeforeUniqueIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE projects(
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 path TEXT NOT NULL DEFAULT '',
 description TEXT NOT NULL DEFAULT '',
 archived INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE TABLE project_tasks(
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL,
 objective TEXT NOT NULL,
 details TEXT NOT NULL DEFAULT '',
 position INTEGER NOT NULL,
 state TEXT NOT NULL,
 target_thread_id TEXT NOT NULL DEFAULT '',
 action_id INTEGER,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 started_at INTEGER,
 completed_at INTEGER
);
INSERT INTO projects(id,name,path,description,archived,created_at,updated_at)
VALUES('p1','Legacy','','',0,1,1);
INSERT INTO project_tasks(id,project_id,objective,position,state,created_at,updated_at) VALUES
('a','p1','A',1,'QUEUED',1,1),
('b','p1','B',1,'QUEUED',2,2),
('c','p1','C',5,'QUEUED',3,3);
`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	items, err := st.ListProjectTasks(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	queued := onlyQueued(items)
	if len(queued) != 3 {
		t.Fatalf("queued=%d want=3", len(queued))
	}
	for i, item := range queued {
		if item.Position != int64(i+1) {
			t.Fatalf("item %s position=%d want=%d", item.ID, item.Position, i+1)
		}
	}
	if queued[0].ID != "a" || queued[1].ID != "b" || queued[2].ID != "c" {
		t.Fatalf("legacy relative order changed: %#v", queued)
	}

	_, err = st.db.Exec(`INSERT INTO project_tasks(id,project_id,objective,position,state,created_at,updated_at) VALUES('duplicate','p1','duplicate',1,'QUEUED',4,4)`)
	if err == nil {
		t.Fatal("expected partial unique queued-position index to reject duplicate position")
	}
}
