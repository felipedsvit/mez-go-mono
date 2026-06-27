// Package settings — service.go: usecase para system_settings (Fase 10 #177).
//
// Service é o ponto único de leitura/escrita de app-level config
// (substitui MEZ_WHATSMEOW_* env vars).
//
// Modelo:
//
//   - Valores são cifrados com a master KEK via Envelope.SealSystem.
//   - Get devolve o valor decifrado (qualquer: string, bool, int, JSON).
//   - Set cifra + persiste + audita + notifica Watchers.
//   - Watch permite hot-reload: subscribers reagem a mudanças.
//
// Defaults são semeados na primeira inicialização (SeedDefaults).
// Quando uma setting é Default-X, Get retorna o default se não houver
// valor no DB.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	pkgcrypto "github.com/felipedsvit/mez-go-mono/pkg/crypto"
)

// Sealer é o subset de pkg/crypto.Envelope que o Service usa.
// Definido como interface para desacoplar de pkg/crypto.
type Sealer interface {
	SealSystem(plaintext []byte) ([]byte, error)
	OpenSystem(ciphertext []byte) ([]byte, error)
}

// Service gerencia system_settings.
type Service struct {
	repo   port.SystemSettingRepository
	sealer Sealer
	log    zerolog.Logger

	mu        sync.RWMutex
	cache     map[string]cachedSetting
	watchers  []chan port.SystemSettingEvent
	kekVersion int
}

type cachedSetting struct {
	value     any
	cachedAt  time.Time
}

// NewService cria o Service. kekVersion é a versão atual da KEK
// (incrementada em rotação, ver cmd/server rotate-kek).
func NewService(repo port.SystemSettingRepository, sealer Sealer, kekVersion int, log zerolog.Logger) *Service {
	return &Service{
		repo:      repo,
		sealer:    sealer,
		log:       log.With().Str("component", "settings.Service").Logger(),
		cache:     make(map[string]cachedSetting),
		kekVersion: kekVersion,
	}
}

// Get lê uma setting. Retorna o valor decifrado ou o default se não
// existir no DB.
//
// Tipo genérico: caller passa o ponteiro para o tipo desejado:
//
//	var enabled bool
//	if err := svc.Get(ctx, "whatsmeow.enabled", &enabled, false); err != nil { ... }
func (s *Service) Get(ctx context.Context, key string, dst any, defaultValue any) error {
	s.mu.RLock()
	if cached, ok := s.cache[key]; ok && time.Since(cached.cachedAt) < 60*time.Second {
		s.mu.RUnlock()
		return assign(dst, cached.value)
	}
	s.mu.RUnlock()

	encrypted, kekVersion, err := s.repo.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("settings get %q: %w", key, err)
	}
	if encrypted == nil {
		// Não existe no DB — usa default.
		return assign(dst, defaultValue)
	}
	if kekVersion != s.kekVersion {
		s.log.Warn().
			Str("key", key).
			Int("db_kek_version", kekVersion).
			Int("current_kek_version", s.kekVersion).
			Msg("settings: KEK version mismatch — KEK rotacionada? Pode precisar de migração")
	}

	plaintext, err := s.sealer.OpenSystem(encrypted)
	if err != nil {
		return fmt.Errorf("settings decrypt %q: %w", key, err)
	}

	var value any
	if err := json.Unmarshal(plaintext, &value); err != nil {
		return fmt.Errorf("settings unmarshal %q: %w", key, err)
	}

	s.mu.Lock()
	s.cache[key] = cachedSetting{value: value, cachedAt: time.Now()}
	s.mu.Unlock()

	return assign(dst, value)
}

// Set persiste + cifra + audita + notifica watchers.
// actor é o email de quem está alterando (admin).
func (s *Service) Set(ctx context.Context, key string, value any, actor string) error {
	return s.setWithDescription(ctx, key, value, "", actor)
}

// SetWithDescription inclui uma descrição legível (ex.: "DSN do whatsmeow").
func (s *Service) SetWithDescription(ctx context.Context, key, description string, value any, actor string) error {
	return s.setWithDescription(ctx, key, value, description, actor)
}

func (s *Service) setWithDescription(ctx context.Context, key string, value any, description, actor string) error {
	plaintext, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("settings marshal %q: %w", key, err)
	}
	encrypted, err := s.sealer.SealSystem(plaintext)
	if err != nil {
		return fmt.Errorf("settings encrypt %q: %w", key, err)
	}

	// Pega o valor anterior (se existir) para o evento.
	var oldEncrypted []byte
	if cached, ok := s.cache[key]; ok {
		oldPlaintext, _ := json.Marshal(cached.value)
		if oldPlaintext != nil {
			oldEncrypted, _ = s.sealer.SealSystem(oldPlaintext)
		}
	}

	if err := s.repo.Set(ctx, key, encrypted, s.kekVersion, description, actor); err != nil {
		return err
	}

	// Audit log: registra a mudança. best-effort — falha aqui não
	// bloqueia a persistência (settings é a fonte da verdade).
	s.log.Info().
		Str("key", key).
		Str("actor", actor).
		Msg("settings: updated")

	// Atualiza cache.
	s.mu.Lock()
	s.cache[key] = cachedSetting{value: value, cachedAt: time.Now()}
	s.mu.Unlock()

	// Notifica watchers.
	s.notifyWatchers(port.SystemSettingEvent{
		Key:           key,
		EncryptedValue: encrypted,
		KekVersion:    s.kekVersion,
		UpdatedBy:     actor,
		OldEncrypted:  oldEncrypted,
	})

	return nil
}

// Delete remove uma setting (admin).
func (s *Service) Delete(ctx context.Context, key, actor string) error {
	if err := s.repo.Delete(ctx, key); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
	s.log.Info().Str("key", key).Str("actor", actor).Msg("settings: deleted")
	return nil
}

// List devolve todas as settings (metadata + valor decifrado).
// Use para o admin panel.
func (s *Service) List(ctx context.Context) ([]SettingView, error) {
	entries, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SettingView, 0, len(entries))
	for _, e := range entries {
		view := SettingView{
			Key:         e.Key,
			Description: e.Description,
			UpdatedAt:   e.UpdatedAt,
			UpdatedBy:   e.UpdatedBy,
			KekVersion:  e.KekVersion,
		}
		plaintext, err := s.sealer.OpenSystem(e.Encrypted)
		if err != nil {
			view.Value = "<decrypt error: " + err.Error() + ">"
		} else {
			// Mostra como JSON cru.
			view.Value = string(plaintext)
		}
		out = append(out, view)
	}
	return out, nil
}

// SettingView é a forma legível de uma setting (para o admin panel).
type SettingView struct {
	Key         string
	Value       string
	Description string
	UpdatedAt   string
	UpdatedBy   string
	KekVersion  int
}

// Watch retorna um canal de eventos + uma função de unsubscribe.
// Use para hot-reload: o subscriber reage a Set() em tempo real.
func (s *Service) Watch() (<-chan port.SystemSettingEvent, func()) {
	ch := make(chan port.SystemSettingEvent, 16)
	s.mu.Lock()
	s.watchers = append(s.watchers, ch)
	s.mu.Unlock()

	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, w := range s.watchers {
			if w == ch {
				s.watchers = append(s.watchers[:i], s.watchers[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, cancel
}

func (s *Service) notifyWatchers(ev port.SystemSettingEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.watchers {
		select {
		case ch <- ev:
		default:
			// Watcher lento: drop. (eventualmente consistente — o
			// subscriber pode chamar Get() periodicamente para
			// reconciliar.)
			s.log.Warn().Str("key", ev.Key).Msg("settings: watcher lento, drop")
		}
	}
}

// InvalidateCache limpa o cache de uma setting (ou todas se key=="*").
// Use após rotação de KEK ou debug.
func (s *Service) InvalidateCache(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "*" {
		s.cache = make(map[string]cachedSetting)
		return
	}
	delete(s.cache, key)
}

// SeedDefaults aplica os defaults passados se a setting não existe.
// Idempotente: roda no boot sem efeito se já populado.
//
// Se defaults==nil e há defaults conhecidos, usa o set mínimo da Fase 10
// (whatsmeow enabled=false + identity chrome + ffmpeg 4).
func (s *Service) SeedDefaults(ctx context.Context, actor string) error {
	defaults := map[string]SettingDefault{
		"whatsmeow.enabled": {
			Value:    false,
			Type:     "bool",
			Desc:     "Liga o canal WhatsApp Web (whatsmeow real). Se false, usa o stub (default dev/staging).",
		},
		"whatsmeow.device_dsn": {
			Value:    "",
			Type:     "string",
			Desc:     "DSN do Postgres para o session store do whatsmeow. Cifrado com a master KEK. Vazio = stub.",
		},
		"whatsmeow.identity.kind": {
			Value:    "chrome",
			Type:     "string",
			Desc:     "Browser emulado para o handshake (anti-ban E1). Valores: chrome, edge, none.",
		},
		"whatsmeow.identity.os": {
			Value:    "Mac OS",
			Type:     "string",
			Desc:     "OS reportado no handshake do whatsmeow. Default Mac OS (mais comum).",
		},
		"ffmpeg.concurrency": {
			Value:    4,
			Type:     "int",
			Desc:     "Limite de subprocessos ffmpeg simultâneos (semaforo). Default 4.",
		},
		"bus.inbound.buffer": {
			Value:    1024,
			Type:     "int",
			Desc:     "Tamanho do buffer inbound do bus in-process.",
		},
		"bus.outbound.buffer": {
			Value:    1024,
			Type:     "int",
			Desc:     "Tamanho do buffer outbound do bus in-process.",
		},
		"reconcile.interval": {
			Value:    "30s",
			Type:     "string",
			Desc:     "Intervalo do Reconciler (recuperação de mensagens não roteadas).",
		},
	}

	for key, def := range defaults {
		encrypted, _, err := s.repo.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("seed %q: %w", key, err)
		}
		if encrypted != nil {
			continue // já existe
		}
		if err := s.setWithDescription(ctx, key, def.Value, def.Desc, actor); err != nil {
			return fmt.Errorf("seed %q: %w", key, err)
		}
	}
	return nil
}

// SettingDefault é o shape de um default para SeedDefaults.
type SettingDefault struct {
	Value any
	Type  string
	Desc  string
}

// assign copia o valor decifrado para dst (via JSON round-trip).
// Garante type-safety: o tipo do JSON deve bater com o tipo do dst.
func assign(dst any, value any) error {
	// JSON round-trip garante conversão correta para o tipo alvo.
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// EnvelopeSealer é um adapter que expõe Envelope via interface Sealer.
type EnvelopeSealer struct {
	*pkgcrypto.Envelope
}

// SealSystem implementa Sealer.
func (e *EnvelopeSealer) SealSystem(plaintext []byte) ([]byte, error) {
	return e.Envelope.SealSystem(plaintext)
}

// OpenSystem implementa Sealer.
func (e *EnvelopeSealer) OpenSystem(ciphertext []byte) ([]byte, error) {
	return e.Envelope.OpenSystem(ciphertext)
}

// NewEnvelopeSealer cria um Sealer a partir de *pkgcrypto.Envelope.
func NewEnvelopeSealer(env *pkgcrypto.Envelope) *EnvelopeSealer {
	return &EnvelopeSealer{Envelope: env}
}

// Sentinel para erros comuns.
var (
	ErrNotFound = errors.New("settings: not found")
)
