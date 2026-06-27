package domain

import "time"

// ChannelCredentials é uma linha da tabela channel_credentials. Persistida
// como (tenant_id, channel) UNIQUE — uma credencial cifrada por canal por
// tenant. É a unidade de leitura/escrita do ChannelCredentialsRepo (#90).
//
// Campos:
//
//   - WrappedDEK: DEK do tenant cifrada pela KEK (nonce||ciphertext).
//     Persistida como BYTEA. Rotada por `cmd/server rotate-kek` (#92).
//
//   - Encrypted: credencial (access token, bot token, ...) cifrada pela
//     DEK. Não muda quando a KEK rotaciona — só o wrap da DEK muda.
//
//   - KEKVersion: qual versão da KEK cifrou WrappedDEK. Default 1 nas
//     linhas pré-Fase 7. Incrementado pelo rotate-kek.
//
//   - RotationWindowUntil: timestamp até o qual a keyring aceita decifrar
//     com a KEK anterior (recovery window — Fase 8+). NIL = sem janela
//     aberta (caso comum: já rotacionado, só KEK nova vale).
type ChannelCredentials struct {
	TenantID             TenantID    `json:"tenant_id"`
	Channel              Channel     `json:"channel"`
	WrappedDEK           []byte      `json:"-"`
	Encrypted            []byte      `json:"-"`
	KEKVersion           int         `json:"kek_version"`
	RotationWindowUntil  *time.Time  `json:"rotation_window_until,omitempty"`
	CreatedAt            time.Time   `json:"created_at"`
	UpdatedAt            time.Time   `json:"updated_at"`
}
