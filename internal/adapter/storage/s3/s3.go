// Package s3 é o adapter de object storage (S3-compatível, MinIO no dev) para
// mídia + backups por tenant. Separa o binário do Postgres (docs/mez-arquitetura.md).
//
// Fase 6 (#84): o mesmo client é usado para mídia (Fase 5) e para o NDJSON de
// backup/restore por tenant. A separação física entre buckets (MEZ_S3_BUCKET
// vs MEZ_S3_BACKUP_BUCKET) garante que um restore não sobrescreva mídia em uso.
package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog"
)

// Config agrupa os parâmetros de conexão.
type Config struct {
	Endpoint     string
	AccessKey    string
	SecretKey    string
	Bucket       string // bucket de mídia
	BackupBucket string // bucket de backup (pode ser igual a Bucket em single-tenant)
	UseSSL       bool
	// Region é opcional — MinIO ignora; S3 real exige (default "us-east-1").
	Region string
}

// Store encapsula o cliente MinIO/S3 e os buckets de mídia + backup.
type Store struct {
	log         zerolog.Logger
	client      *minio.Client
	bucket      string
	backupBucket string
	// publicBase é o prefixo de URL para compor a URL pública do objeto
	// (ex.: http://localhost:9000/mez-media). Em produção, um CDN/endpoint público.
	publicBase string
}

// New cria o Store e garante a existência dos buckets. Se Endpoint estiver
// vazio, retorna um Store no-op (nil error) para que binários de migração e
// testes que não tocam S3 não precisem do serviço rodando.
func New(ctx context.Context, log zerolog.Logger, cfg Config) (*Store, error) {
	if cfg.Endpoint == "" {
		log.Warn().Msg("s3: endpoint vazio — store em modo no-op")
		return &Store{log: log}, nil
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3: bucket de mídia é obrigatório")
	}
	if cfg.BackupBucket == "" {
		cfg.BackupBucket = cfg.Bucket
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("criar cliente s3: %w", err)
	}

	for _, b := range []string{cfg.Bucket, cfg.BackupBucket} {
		exists, err := client.BucketExists(ctx, b)
		if err != nil {
			return nil, fmt.Errorf("checar bucket %q: %w", b, err)
		}
		if !exists {
			if err := client.MakeBucket(ctx, b, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
				return nil, fmt.Errorf("criar bucket %q: %w", b, err)
			}
			log.Info().Str("bucket", b).Msg("s3: bucket criado")
		}
	}

	scheme := "http"
	if cfg.UseSSL {
		scheme = "https"
	}
	return &Store{
		log:          log.With().Str("component", "s3.Store").Logger(),
		client:       client,
		bucket:       cfg.Bucket,
		backupBucket: cfg.BackupBucket,
		publicBase:   fmt.Sprintf("%s://%s/%s", scheme, cfg.Endpoint, cfg.Bucket),
	}, nil
}

// MediaBucket retorna o nome do bucket de mídia.
func (s *Store) MediaBucket() string { return s.bucket }

// BackupBucket retorna o nome do bucket de backup.
func (s *Store) BackupBucket() string { return s.backupBucket }

// Client expõe o cliente minio para adapters que precisem de operações
// avançadas (ex.: multipart upload streaming).
func (s *Store) Client() *minio.Client { return s.client }

// Put grava o objeto no bucket de mídia e devolve sua URL pública.
func (s *Store) Put(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("s3: store em modo no-op")
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("put object %q: %w", key, err)
	}
	return s.publicBase + "/" + key, nil
}

// Get lê o objeto do bucket de mídia e seu content-type.
func (s *Store) Get(ctx context.Context, key string) ([]byte, string, error) {
	if s.client == nil {
		return nil, "", fmt.Errorf("s3: store em modo no-op")
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("get object %q: %w", key, err)
	}
	defer obj.Close()

	// Stat() antes de ReadAll: io.ReadAll consome o stream até EOF e, no
	// minio-go, Stat() após a leitura devolve metadata esvaziada. Chamado
	// antes, Stat() também faz o HTTP HEAD subjacente expor o erro 404 real
	// (objeto inexistente) em vez de um erro de leitura opaco.
	info, err := obj.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("stat object %q: %w", key, err)
	}
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, "", fmt.Errorf("ler object %q: %w", key, err)
	}
	return data, info.ContentType, nil
}

// Delete remove o objeto do bucket de mídia. Idempotente (erro 404 ignorado).
func (s *Store) Delete(ctx context.Context, key string) error {
	if s.client == nil {
		return nil
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		// 404 é tolerado (idempotência do reset)
		if !isNotFound(err) {
			return fmt.Errorf("delete object %q: %w", key, err)
		}
	}
	return nil
}

// PublicURL compõe a URL pública sem fazer I/O.
func (s *Store) PublicURL(key string) string {
	return s.publicBase + "/" + key
}

// ParseS3URL extrai a chave de uma URL pública do Store. Útil no restore para
// descobrir o objeto S3 a partir da URL gravada no banco.
func (s *Store) ParseS3URL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	// path = /<bucket>/<key>
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("url %q sem chave válida", raw)
	}
	return parts[1], nil
}

// isNotFound detecta erro 404 do minio-go (comparação de mensagem para
// evitar importar o pacote de erros tipado que muda entre versões).
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") ||
		strings.Contains(msg, "404") ||
		strings.Contains(msg, "Not Found")
}
