-- 0006_kek_version.up.sql
-- Fase 7 (#89): versionamento de KEK em channel_credentials.
--
-- Necessário para suportar cmd/server rotate-kek (issue #92). Cada linha
-- passa a registrar:
--   - kek_version: qual versão da KEK cifrou o wrapped_dek atual
--   - rotation_window_until: até quando o sistema aceita tanto a versão
--     antiga quanto a nova (recovery window — Fase 8+)
--
-- Linhas existentes recebem kek_version=1 (default), significando que foram
-- cifradas com a KEK inicial (a única que existia até a Fase 7).
--
-- mez_platform precisa de SELECT/INSERT/UPDATE/DELETE em channel_credentials
-- para o `rotate-kek` (que itera todos os tenants via RunAsPlatform, #92).
-- O GRANT abaixo re-assegura a permissão mesmo se migrações anteriores
-- tiverem revogado por engano.

BEGIN;

ALTER TABLE channel_credentials
    ADD COLUMN IF NOT EXISTS kek_version INT NOT NULL DEFAULT 1;

ALTER TABLE channel_credentials
    ADD COLUMN IF NOT EXISTS rotation_window_until TIMESTAMPTZ;

-- Index parcial: cobre apenas linhas em janela de rotação (minoria). O
-- reconciler de rotação (#92) e o Keyring (issue #91) usam-no para listar
-- rapidamente credenciais que ainda podem ser decifradas com a KEK antiga.
CREATE INDEX IF NOT EXISTS idx_channel_credentials_kek_version
    ON channel_credentials(kek_version) WHERE rotation_window_until IS NOT NULL;

-- mez_platform precisa ler/escrever para RunAsPlatform em rotate-kek.
-- O grant ALL TABLES em 0001 já cobre isso, mas re-assegurar é defensivo
-- contra migrations futuras que possam revogar.
GRANT SELECT, INSERT, UPDATE, DELETE ON channel_credentials TO mez_platform;

COMMIT;
