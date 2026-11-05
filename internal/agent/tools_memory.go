package agent

import "strings"

// memoryTools edit Alexa's long-term memory of the user (memory.go) and
// search what was said before the summary (recall.go).
func memoryTools() []*tool {
	return []*tool{
		{
			Name:        "memory_save",
			Description: `Remember a lasting fact about the user for all future conversations. Memory is shown to you on every turn and has a size budget, so keep each entry to one short sentence. Write a fact ("Likes a window seat"), not an order to yourself. To correct, update or merge entries, list their ids in replaces: they're removed and this entry is added in one step, and the budget is checked after both. Never store passwords, codes, card or account numbers.`,
			Params: map[string]any{
				"content":  p("string", "The fact, one short self-contained sentence, under 200 characters"),
				"replaces": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Ids of entries this one corrects or merges; removed in the same step"},
			},
			Required: []string{"content"},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				if tc.run.memoryStopped() {
					return &toolResult{Text: memoryStop}, nil
				}
				res, err := tc.rt.editMemory(tc.ctx, tc.run.user.ID, "alexa", memEdit{Content: strings.TrimSpace(str(a, "content")), Replaces: intList(a, "replaces")})
				if err != nil {
					return nil, err
				}
				return tc.rt.memoryResult(tc, res), nil
			},
		},
		{
			Name:        "memory_delete",
			Description: "Delete memory entries that are wrong or no longer true, by id.",
			Params:      map[string]any{"ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Ids of the entries to delete"}},
			Required:    []string{"ids"},
			Run: func(tc *toolCtx, a map[string]any) (*toolResult, error) {
				if tc.run.memoryStopped() {
					return &toolResult{Text: memoryStop}, nil
				}
				ids := intList(a, "ids")
				if len(ids) == 0 {
					ids = intList(a, "id") // as it was called before it took a list
				}
				res, err := tc.rt.deleteMemories(tc.ctx, tc.run.user.ID, ids)
				if err != nil {
					return nil, err
				}
				return tc.rt.memoryResult(tc, res), nil
			},
		},
		recallTool(), // recall.go
	}
}
