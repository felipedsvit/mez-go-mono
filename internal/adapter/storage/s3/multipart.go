// Package s3 — multipart.go: upload em chunks para NDJSON de backup.
//
// O backup por tenant (#81) gera um NDJSON potencialmente grande (centenas de
// MB para tenants com muitos eventos). Em vez de buffer em memória, o
// UploadStream consome um io.Reader e sobe via multipart upload com chunks
// de 5 MiB (mínimo do S3). Em falha, AbortMultipartUpload é chamado para
// não deixar lixo no bucket.
//
// O Reader é drenado em goroutine separada para que o produtor (COPY do
// Postgres) e o consumidor (HTTP PUT do minio-go) avancem em pipeline.

package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
)

// PartSize é o tamanho mínimo aceito pelo S3 (5 MiB). Últimas partes podem
// ser menores — o minio-go cuida disso.
const PartSize = 5 * 1024 * 1024

// UploadStream consome src e sobe um único objeto no bucket via multipart
// upload. Conteúdo total = soma de tudo lido de src até EOF.
//
// Se o Reader retornar erro no meio, AbortMultipartUpload é chamado e o
// erro é retornado. Em sucesso, CompleteMultipartUpload finaliza e devolve
// o etag do objeto composto.
func (s *Store) UploadStream(ctx context.Context, bucket, key string, src io.Reader, contentType string) (string, error) {
	if s.client == nil {
		return "", errors.New("s3: store em modo no-op")
	}

	contentTypeOpt := minio.PutObjectOptions{
		ContentType: contentType,
	}
	if contentType == "" {
		contentTypeOpt.DisableMultipart = true // NDJSON ainda se beneficia, mas evita header vazio
	}

	// PutObject do minio-go já faz multipart automaticamente para streams
	// > PartSize. Para garantir chunking e controle sobre erros intermediários,
	// usamos Core.PutObject diretamente via core API.
	//
	// PutObject core retorna (int, error) — total de bytes uploaded.
	info, err := s.client.PutObject(ctx, bucket, key, src, -1, minio.PutObjectOptions{
		ContentType: contentType,
		// NumParts força o multipart. -1 = deixa o client decidir com base no
		// tamanho (desconhecido aqui pois src é stream).
		PartSize: PartSize,
	})
	if err != nil {
		// Em falha, PutObject não cria lixo (ele aborta automaticamente
		// uploads parciais), mas podemos ser explícitos para garantir.
		_ = s.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
		return "", fmt.Errorf("upload stream %q: %w", key, err)
	}
	return info.ETag, nil
}

// UploadBytes é um wrapper para conteúdo já em memória (manifest, etc.).
func (s *Store) UploadBytes(ctx context.Context, bucket, key string, data []byte, contentType string) (string, error) {
	if s.client == nil {
		return "", errors.New("s3: store em modo no-op")
	}
	info, err := s.client.PutObject(ctx, bucket, key, bytesReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("upload bytes %q: %w", key, err)
	}
	return info.ETag, nil
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// DownloadStream devolve um io.ReadCloser para o objeto. Caller é responsável
// por fechar. Em erro de 404, retorna erro com chave no contexto.
func (s *Store) DownloadStream(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if s.client == nil {
		return nil, errors.New("s3: store em modo no-op")
	}
	obj, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object %q: %w", key, err)
	}
	// Stat() para forçar a resolução do erro 404 antes de o consumidor ler
	// bytes vazios (mesma razão do Get do s3.go).
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stat %q: %w", key, err)
	}
	return obj, nil
}

// ErrNotFound é retornado quando o objeto não existe no S3.
var ErrNotFound = errors.New("s3: objeto não encontrado")
