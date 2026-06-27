package domain

// ACD types (Agent, Queue, RoutingStrategy, RequiredSkill) portados
// do mez-go pai. Usados pelo routing.Router quando o ACD está ligado.

// AgentID identifica um agente.
type AgentID string

// AgentStatus é o estado do agente.
type AgentStatus string

const (
	// AgentOnline: pode receber conversas.
	AgentOnline AgentStatus = "online"
	// AgentOffline: pausado/ausente.
	AgentOffline AgentStatus = "offline"
)

// Agent é o atendente de uma fila. Tem skills, capacidade e carga atual.
type Agent struct {
	ID          AgentID     `json:"id"`
	TenantID    TenantID    `json:"tenant_id"`
	Name        string      `json:"name"`
	Status      AgentStatus `json:"status"`
	MaxLoad     int         `json:"max_load"`
	CurrentLoad int         `json:"current_load"`
	Skills      []Skill     `json:"skills,omitempty"`
}

// HasCapacity devolve true se o agente ainda tem slot livre.
func (a Agent) HasCapacity() bool { return a.CurrentLoad < a.MaxLoad }

// SkillLevel é o nível de proficiência de um agente em uma skill.
type SkillLevel int

const (
	SkillLevelBasic     SkillLevel = 1
	SkillLevelAdvanced  SkillLevel = 2
	SkillLevelExpert    SkillLevel = 3
)

// Skill é uma capacidade do agente (ex.: "espanhol" nível 2).
type Skill struct {
	Name  string     `json:"name"`
	Level SkillLevel `json:"level"`
}

// MeetsSkills devolve true se o agente tem todas as skills no nível mínimo.
func (a Agent) MeetsSkills(required []RequiredSkill) bool {
	if len(required) == 0 {
		return true
	}
	have := make(map[string]SkillLevel, len(a.Skills))
	for _, s := range a.Skills {
		have[s.Name] = s.Level
	}
	for _, r := range required {
		if have[r.Name] < r.MinLevel {
			return false
		}
	}
	return true
}

// RequiredSkill é uma skill mínima que o agente deve ter.
type RequiredSkill struct {
	Name     string     `json:"name"`
	MinLevel SkillLevel `json:"min_level"`
}

// QueueID identifica uma fila.
type QueueID string

// RoutingStrategy é a estratégia de seleção do próximo agente.
type RoutingStrategy string

const (
	// StrategyRoundRobin: rotaciona entre elegíveis (mod soma de cargas).
	StrategyRoundRobin RoutingStrategy = "round_robin"
	// StrategyLeastBusy: sempre o de menor carga atual.
	StrategyLeastBusy RoutingStrategy = "least_busy"
	// StrategySkillBased: filtra por skills e desempata por menor carga.
	StrategySkillBased RoutingStrategy = "skill_based"
	// StrategySticky: prioriza o agente que já atendeu o contato.
	StrategySticky RoutingStrategy = "sticky"
)

// Queue é uma fila de atendimento.
type Queue struct {
	ID              QueueID         `json:"id"`
	TenantID        TenantID        `json:"tenant_id"`
	Name            string          `json:"name"`
	Strategy        RoutingStrategy `json:"strategy"`
	OverflowQueueID QueueID         `json:"overflow_queue_id,omitempty"`
	IsDefault       bool            `json:"is_default"`
}
