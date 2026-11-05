package api

import (
	"fmt"
	"testing"
)

// The memory endpoints work as before, and each entry now says who wrote
// it and when it last changed. A delete can be undone.
func TestMemoryEndpoints(t *testing.T) {
	e := newTestServer(t)
	c := e.client(t)
	c.signup("mia")
	code, m, _ := c.do("POST", "/api/memories", map[string]any{"content": "  Prefers quiet hotels "}, true)
	if code != 200 || m["content"] != "Prefers quiet hotels" || m["source"] != "user" || m["updatedAt"] == nil || m["id"] == nil {
		t.Fatalf("add = %d %v", code, m)
	}
	id := fmt.Sprint(m["id"])
	c.do("PUT", "/api/memories/"+id, map[string]any{"content": "Prefers quiet hotels near parks"}, true)
	_, _, list := c.do("GET", "/api/memories", nil, false)
	if len(list) != 1 || list[0].(map[string]any)["content"] != "Prefers quiet hotels near parks" {
		t.Fatalf("after update: %v", list)
	}
	if code, _, _ := c.do("DELETE", "/api/memories/"+id, nil, true); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	if _, _, list := c.do("GET", "/api/memories", nil, false); len(list) != 0 {
		t.Fatalf("a deleted entry is still listed: %v", list)
	}
	if code, _, _ := c.do("POST", "/api/memories/"+id+"/restore", nil, true); code != 200 {
		t.Fatalf("restore = %d", code)
	}
	if _, _, list := c.do("GET", "/api/memories", nil, false); len(list) != 1 {
		t.Fatalf("after restore: %v", list)
	}
	if code, _, _ := c.do("POST", "/api/memories/"+id+"/restore", nil, true); code != 404 {
		t.Fatalf("restoring a live entry = %d, want 404", code)
	}
}
