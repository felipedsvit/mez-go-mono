// Package s3 — delete.go: deleção recursiva por prefixo.
//
// Usado no reset por tenant (#83): após deletar os dados do tenant no DB, o
// S3 também precisa ser limpo (mídia em tenants/<id>/ + backups em
// backups/tenants/<id>/). ListObjects é recursivo e RemoveObjects é batch.

package s3

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
)

// DeletePrefix remove todos os objetos cujo key começa com prefix.
//
// Estratégia: listar em chunks de 1000, acumular, chamar RemoveObjects em
// batch (até 1000 chaves por chamada). Retorna o total removido. Idempotente
// (objetos inexistentes são ignorados pelo minio-go).
func (s *Store) DeletePrefix(ctx context.Context, bucket, prefix string) (int, error) {
	if s.client == nil {
		return 0, nil
	}

	objectsCh := s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	// RemoveObjects em batch.
	const batchSize = 1000
	var keys []minio.ObjectInfo
	total := 0
	for obj := range objectsCh {
		keys = append(keys, obj)
		if len(keys) >= batchSize {
			if err := s.removeBatch(ctx, bucket, keys); err != nil {
				return total, err
			}
			total += len(keys)
			keys = keys[:0]
		}
	}
	if len(keys) > 0 {
		if err := s.removeBatch(ctx, bucket, keys); err != nil {
			return total, err
		}
		total += len(keys)
	}
	return total, nil
}

func (s *Store) removeBatch(ctx context.Context, bucket string, keys []minio.ObjectInfo) error {
	objsCh := make(chan minio.ObjectInfo, len(keys))
	for _, k := range keys {
		objsCh <- k
	}
	close(objsCh)

	errCh := s.client.RemoveObjects(ctx, bucket, objsCh, minio.RemoveObjectsOptions{})
	// RemoveObjects retorna um canal de erros por chave; ignoramos erros 404
	// (idempotência) e logamos o resto.
	for e := range errCh {
		if !isNotFound(e.Err) {
			return fmt.Errorf("remove %q: %w", e.ObjectName, e.Err)
		}
	}
	return nil
}
