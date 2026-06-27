#!/bin/sh
set -e

# Fase 8 #99 sub-issue: migrations são executadas inline pelo
# `serve` (via runMigrateInline) quando MEZ_MIGRATE_ON_BOOT=true
# (default). Em prod, desligue MEZ_MIGRATE_ON_BOOT e rode migrations
# em job separado.
#
# Antes: `migrate up && serve` (2 processos, race entre o fim do
# migrate e a abertura do serve).
# Agora: `serve` (1 processo, atomic — falha no migrate aborta o
# boot antes de qualquer serviço subir).
exec mez-go-mono serve
