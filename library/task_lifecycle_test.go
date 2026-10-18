package library

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/tasks"
)

func TestRESTTaskLifecycle(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	token, err := CreateToken(ctx, db, "lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	store := taskdb.NewStore(db)
	for _, purge := range []bool{false, true} {
		id := fmt.Sprintf("lifecycle-%t", purge)
		for _, method := range []string{"POST", "PUT"} {
			path := "/api/v1/tasks"
			want := 201
			if method == "PUT" {
				path += "/" + id
				want = 200
			}
			w := request(r, method, path, `{"id":"`+id+`","title":"first","created_at":1000,"updated_at":1000,"last_modified":1000}`, token)
			if w.Code != want {
				t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
			}
		}
		before, err := store.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		w := request(r, "POST", "/api/v1/tasks", `{"id":"`+id+`","title":"changed","updated_at":1000,"last_modified":1000}`, token)
		if w.Code != 201 {
			t.Fatal(w.Code)
		}
		var saved tasks.Task
		if err = json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
			t.Fatal(err)
		}
		if saved.CreatedAt != before.CreatedAt || saved.UpdatedAt <= before.UpdatedAt || saved.LastModified != saved.UpdatedAt {
			t.Fatalf("invalid stamps: %+v", saved)
		}
		if err = store.Delete(ctx, id); err != nil {
			t.Fatal(err)
		}
		if purge {
			if _, err = store.Purge(ctx, []string{id}); err != nil {
				t.Fatal(err)
			}
		}
		for _, method := range []string{"POST", "PUT"} {
			path := "/api/v1/tasks"
			if method == "PUT" {
				path += "/" + id
			}
			if w := request(r, method, path, `{"id":"`+id+`","title":"resurrected"}`, token); w.Code != 409 {
				t.Fatalf("purge=%t %s status=%d", purge, method, w.Code)
			}
		}
		if !purge {
			if err = store.Restore(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, method := range []string{"POST", "PUT"} {
		path := "/api/v1/tasks"
		if method == "PUT" {
			path += "/bad"
		}
		if w := request(r, method, path, `{"id":"bad","deleted":true}`, token); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
