// Package backup — job.go: store in-memory de jobs de backup/restore/reset.
//
// Jobs são de curta duração (segundos a minutos). Mantemos em memória +
// Recovery no boot (carrega manifests S3 existentes para List). Sem tabela
// no DB — o estado canônico é o S3 (manifest.json + NDJSON lá).

package backup

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// JobKind distingue os 3 tipos.
type JobKind string

const (
	JobExport  JobKind = "export"
	JobRestore JobKind = "restore"
	JobReset   JobKind = "reset"
)

// JobState é o estado de execução. Segue um FSM linear:
// pending → running → (done | failed).
type JobState string

const (
	StatePending JobState = "pending"
	StateRunning JobState = "running"
	StateDone    JobState = "done"
	StateFailed  JobState = "failed"
)

// TableProgress rastreia o progresso por tabela no export/restore.
type TableProgress struct {
	Name  string `json:"name"`
	Rows  int64  `json:"rows"`
	State JobState `json:"state"`
}

// Job representa uma operação de backup/restore/reset em andamento ou
// concluída. Mutado apenas pela goroutine que o executa; leituras
// externas via JobStore.Get/List são protegidas por RLock.
//
// lock é o mutex interno: as goroutines de export/restore/reset mutam o
// state/progress frequentemente (por tabela), e queremos leituras
// simultâneas via JobStore.Get sem race.
type Job struct {
	lock        sync.Mutex
	ID          string          `json:"id"`
	Kind        JobKind         `json:"kind"`
	TenantID    string          `json:"tenant_id"`
	Actor       string          `json:"actor"` // email do admin
	State       JobState        `json:"state"`
	ProgressPct int             `json:"progress_pct"` // 0..100
	CurrentStep string          `json:"current_step"`
	Tables      []TableProgress `json:"tables,omitempty"`
	Error       string          `json:"error,omitempty"`
	BackupID    string          `json:"backup_id,omitempty"` // para restore: qual backup
	StartedAt   time.Time       `json:"started_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
}

// Lock devolve o mutex interno. Usado pelas goroutines que executam o job
// para mutar campos com segurança.
func (j *Job) Lock() *sync.Mutex { return &j.lock }

// snapshot devolve uma cópia consistente dos campos relevantes para
// consumidores (status do job). Caller-safe: protegida por lock interno.
func (j *Job) snapshot() (state JobState, progress int, currentStep, errMsg string, tables []TableProgress, finishedAt *time.Time, updatedAt time.Time) {
	j.lock.Lock()
	defer j.lock.Unlock()
	return j.State, j.ProgressPct, j.CurrentStep, j.Error, j.Tables, j.FinishedAt, j.UpdatedAt
}

// ErrJobNotFound é retornado pelo Get quando o ID não existe (ou já foi
// purgado pela janela de retenção).
var ErrJobNotFound = errors.New("backup: job not found")

// JobStore é um store thread-safe de jobs em memória.
//
// Concorrência: cada Job tem sua própria mutex interna (estado mutado pela
// goroutine que o executa). A map é protegida por RWMutex para Lookup.
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	// retention corta entries terminados depois deste tempo (default 1h).
	retention time.Duration
}

// NewJobStore cria o store.
func NewJobStore(retention time.Duration) *JobStore {
	if retention <= 0 {
		retention = time.Hour
	}
	return &JobStore{
		jobs:      make(map[string]*Job),
		retention: retention,
	}
}

// Put adiciona ou substitui um job.
func (s *JobStore) Put(j *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
}

// Get retorna o job pelo ID.
func (s *JobStore) Get(id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	return j, nil
}

// List devolve jobs em ordem reversa de UpdatedAt (mais recente primeiro).
// Limit <= 0 = sem limite.
func (s *JobStore) List(limit int) []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	sort.Slice(out, func(i, k int) bool {
		return out[i].UpdatedAt.After(out[k].UpdatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Purge remove jobs terminados (done|failed) com FinishedAt > retention atrás.
// Chamado periodicamente pelo Service (StartJanitor).
func (s *JobStore) Purge() int {
	cutoff := time.Now().Add(-s.retention)
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, j := range s.jobs {
		if (j.State == StateDone || j.State == StateFailed) &&
			j.FinishedAt != nil && j.FinishedAt.Before(cutoff) {
			delete(s.jobs, id)
			removed++
		}
	}
	return removed
}

// StartJanitor dispara Purge a cada interval. Para com ctx.Done.
func (s *JobStore) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Purge()
			}
		}
	}()
}
