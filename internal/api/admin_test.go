package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"alexagent/internal/userenv"
)

// Deleting a user takes their rows and folders, and nothing makes the
// folders again; an unknown user is a 404 (BH-5).
func TestDeleteUser(t *testing.T) {
	e := newTestServer(t)
	admin := e.client(t)
	admin.signup("alice")
	code, m, _ := admin.do("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "password2"}, true)
	if code != 200 {
		t.Fatalf("create bob: %d %v", code, m)
	}
	id := int64(m["id"].(float64))
	bob := e.client(t)
	bob.post("/api/auth/login", map[string]any{"username": "bob", "password": "password2"})
	env := e.envs.For(id)
	if err := os.WriteFile(env.Home+"/notes.txt", []byte("bob's"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code, _, _ := admin.do("DELETE", "/api/admin/users/999", nil, true); code != 404 {
		t.Errorf("deleting an unknown user = %d, want 404", code)
	}
	if code, m, _ := admin.do("DELETE", fmt.Sprintf("/api/admin/users/%d", id), nil, true); code != 200 {
		t.Fatalf("delete bob: %d %v", code, m)
	}
	if _, err := e.st.GetUser(context.Background(), id); err == nil {
		t.Error("bob's row is still there")
	}
	for _, d := range []string{env.Root, env.Shots, env.Cache} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", d, err)
		}
	}
	if _, err := e.envs.Ensure(id); !errors.Is(err, userenv.ErrRemoved) {
		t.Errorf("Ensure for the deleted user = %v, want ErrRemoved", err)
	}
	if code, _, _ := bob.do("GET", "/api/session", nil, false); code != 401 {
		t.Errorf("bob's session after the delete = %d, want 401", code)
	}
	if code, _, _ := admin.do("DELETE", fmt.Sprintf("/api/admin/users/%d", id), nil, true); code != 404 {
		t.Errorf("deleting bob again = %d, want 404", code)
	}
}
