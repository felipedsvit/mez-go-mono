package routing

import (
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

func mkAgent(id string, status domain.AgentStatus, load, cap int, skills ...domain.Skill) domain.Agent {
	return domain.Agent{
		ID:          domain.AgentID(id),
		Status:      status,
		CurrentLoad: load,
		MaxLoad:     cap,
		Skills:      skills,
	}
}

func TestSelect_NoEligible(t *testing.T) {
	t.Parallel()

	cases := map[string][]domain.Agent{
		"all offline":     {mkAgent("a", domain.AgentOffline, 0, 5)},
		"all at capacity": {mkAgent("a", domain.AgentOnline, 5, 5)},
		"empty":           {},
	}
	for name, candidates := range cases {
		name, candidates := name, candidates
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := Select(SelectInput{Strategy: domain.StrategyLeastBusy, Candidates: candidates}); ok {
				t.Fatalf("expected no eligible agent, got ok=true")
			}
		})
	}
}

func TestSelect_LeastBusy(t *testing.T) {
	t.Parallel()

	in := SelectInput{
		Strategy: domain.StrategyLeastBusy,
		Candidates: []domain.Agent{
			mkAgent("a", domain.AgentOnline, 3, 5),
			mkAgent("b", domain.AgentOnline, 1, 5),
			mkAgent("c", domain.AgentOnline, 2, 5),
		},
	}
	got, ok := Select(in)
	if !ok || got.ID != "b" {
		t.Fatalf("expected b (lowest load), got %q ok=%v", got.ID, ok)
	}
}

func TestSelect_LeastBusy_TieDeterministic(t *testing.T) {
	t.Parallel()

	in := SelectInput{
		Strategy: domain.StrategyLeastBusy,
		Candidates: []domain.Agent{
			mkAgent("z", domain.AgentOnline, 1, 5),
			mkAgent("a", domain.AgentOnline, 1, 5),
		},
	}
	for i := 0; i < 10; i++ {
		got, _ := Select(in)
		if got.ID != "a" {
			t.Fatalf("tie should be deterministic (a), got %q", got.ID)
		}
	}
}

func TestSelect_RoundRobin(t *testing.T) {
	t.Parallel()

	candidates := []domain.Agent{
		mkAgent("a", domain.AgentOnline, 0, 5),
		mkAgent("b", domain.AgentOnline, 0, 5),
		mkAgent("c", domain.AgentOnline, 0, 5),
	}
	want := []string{"a", "b", "c", "a"}
	for seed, w := range want {
		got, ok := Select(SelectInput{Strategy: domain.StrategyRoundRobin, Candidates: candidates, RoundRobinSeed: seed})
		if !ok || string(got.ID) != w {
			t.Errorf("seed=%d expected %q, got %q ok=%v", seed, w, got.ID, ok)
		}
	}
}

func TestSelect_Sticky_FallbackToLeastBusy(t *testing.T) {
	t.Parallel()

	in := SelectInput{
		Strategy:      domain.StrategySticky,
		StickyAgentID: "z", // não está nos elegíveis
		Candidates: []domain.Agent{
			mkAgent("a", domain.AgentOnline, 3, 5),
			mkAgent("b", domain.AgentOnline, 1, 5),
		},
	}
	got, ok := Select(in)
	if !ok || got.ID != "b" {
		t.Errorf("expected fallback to least busy b, got %q", got.ID)
	}
}

func TestSelect_Sticky_Match(t *testing.T) {
	t.Parallel()

	in := SelectInput{
		Strategy:      domain.StrategySticky,
		StickyAgentID: "a",
		Candidates: []domain.Agent{
			mkAgent("a", domain.AgentOnline, 2, 5),
			mkAgent("b", domain.AgentOnline, 1, 5),
		},
	}
	got, ok := Select(in)
	if !ok || got.ID != "a" {
		t.Errorf("expected sticky a, got %q", got.ID)
	}
}

func TestSelect_SkillBased(t *testing.T) {
	t.Parallel()

	in := SelectInput{
		Strategy: domain.StrategySkillBased,
		Required: []domain.RequiredSkill{
			{Name: "espanhol", MinLevel: domain.SkillLevelBasic},
		},
		Candidates: []domain.Agent{
			mkAgent("a", domain.AgentOnline, 0, 5), // sem skill
			mkAgent("b", domain.AgentOnline, 0, 5, domain.Skill{Name: "espanhol", Level: domain.SkillLevelBasic}),
		},
	}
	got, ok := Select(in)
	if !ok || got.ID != "b" {
		t.Errorf("expected b (with skill), got %q", got.ID)
	}
}

func TestEligibleAgents_FiltersOfflineAndFull(t *testing.T) {
	t.Parallel()

	in := []domain.Agent{
		mkAgent("a", domain.AgentOnline, 0, 5),
		mkAgent("b", domain.AgentOffline, 0, 5),
		mkAgent("c", domain.AgentOnline, 5, 5), // full
	}
	eligible := eligibleAgents(in, nil)
	if len(eligible) != 1 || eligible[0].ID != "a" {
		t.Errorf("expected only a, got %+v", eligible)
	}
}

func TestFindByID(t *testing.T) {
	t.Parallel()

	agents := []domain.Agent{
		mkAgent("a", domain.AgentOnline, 0, 5),
		mkAgent("b", domain.AgentOnline, 0, 5),
	}
	if _, ok := findByID(agents, "a"); !ok {
		t.Error("expected to find a")
	}
	if _, ok := findByID(agents, ""); ok {
		t.Error("empty id should not match")
	}
	if _, ok := findByID(agents, "c"); ok {
		t.Error("c should not match")
	}
}

func TestRoundRobinSeed(t *testing.T) {
	t.Parallel()

	candidates := []domain.Agent{
		mkAgent("a", domain.AgentOnline, 1, 5),
		mkAgent("b", domain.AgentOnline, 3, 5),
		mkAgent("c", domain.AgentOnline, 2, 5),
	}
	if got := roundRobinSeed(candidates); got != 6 {
		t.Errorf("sum of loads = %d, want 6", got)
	}
}

func TestMeetsSkills(t *testing.T) {
	t.Parallel()

	a := mkAgent("a", domain.AgentOnline, 0, 5,
		domain.Skill{Name: "espanhol", Level: domain.SkillLevelAdvanced},
		domain.Skill{Name: "ingles", Level: domain.SkillLevelBasic},
	)
	if !a.MeetsSkills([]domain.RequiredSkill{{Name: "espanhol", MinLevel: domain.SkillLevelBasic}}) {
		t.Error("should meet basic espanhol")
	}
	if !a.MeetsSkills([]domain.RequiredSkill{{Name: "espanhol", MinLevel: domain.SkillLevelAdvanced}}) {
		t.Error("should meet advanced espanhol")
	}
	if a.MeetsSkills([]domain.RequiredSkill{{Name: "espanhol", MinLevel: domain.SkillLevelExpert}}) {
		t.Error("should NOT meet expert espanhol")
	}
	if !a.MeetsSkills(nil) {
		t.Error("empty required = always meet")
	}
}
