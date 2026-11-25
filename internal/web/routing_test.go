package web

import (
	"testing"

	"fastasiapanel/internal/store"
)

func TestMoveRuleEqualPriorities(t *testing.T) {
	rules := []store.RoutingRule{
		{ID: "a", OutboundTag: "direct", Priority: 50},
		{ID: "b", OutboundTag: "direct", Priority: 50},
		{ID: "c", OutboundTag: "direct", Priority: 50},
	}
	if err := moveRule(rules, "c", "up"); err != nil {
		t.Fatalf("moveRule failed: %v", err)
	}
	wantIDs := []string{"a", "c", "b"}
	wantPris := []int{10, 20, 30}
	for i := range rules {
		if rules[i].ID != wantIDs[i] || rules[i].Priority != wantPris[i] {
			t.Fatalf("at %d got (%s, %d), want (%s, %d)", i, rules[i].ID, rules[i].Priority, wantIDs[i], wantPris[i])
		}
	}
}
