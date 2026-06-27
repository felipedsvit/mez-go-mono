package waba

import (
	"context"
	"fmt"
	"strings"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// MediaStore persiste a mídia recebida (implementado por internal/adapter/storage/s3).
type MediaStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) (string, error)
}

// Media baixa a mídia inbound da Cloud API (2 passos) e a persiste no store. É
// usada pelo webhook (assíncrono) p/ não atrasar o 200 que a Meta exige.
type Media struct {
	client *Client
	store  MediaStore
}

// NewMedia cria o enriquecedor de mídia. store pode vir do adapter s3.
func NewMedia(client *Client, store MediaStore) *Media {
	return &Media{client: client, store: store}
}

// InboundMediaRef é a referência mínima de uma mídia inbound que precisa ser
// enriquecida (baixada + persistida). Equivalente ao event.InboundEvent.Payload
// estendido: media_id, channel, tenant, message_id.
type InboundMediaRef struct {
	TenantID          domain.TenantID
	Channel           domain.Channel
	ProviderMessageID string
	Metadata          map[string]any
}

// Enrich baixa a mídia referenciada por Metadata["media_id"], grava no store e
// preenche Metadata["media_url"]. Best-effort: sem media_id ou store, é no-op.
func (md *Media) Enrich(ctx context.Context, ref *InboundMediaRef) error {
	if md == nil || md.store == nil || ref == nil {
		return nil
	}
	mediaID, _ := ref.Metadata["media_id"].(string)
	if mediaID == "" {
		return nil
	}

	url, mime, err := md.client.GetMediaURL(ctx, mediaID)
	if err != nil {
		return fmt.Errorf("obter url da mídia: %w", err)
	}
	data, err := md.client.DownloadMedia(ctx, url)
	if err != nil {
		return fmt.Errorf("baixar mídia: %w", err)
	}

	key := fmt.Sprintf("%s/%s/%s.%s", ref.TenantID, ref.Channel, ref.ProviderMessageID, extFromMime(mime))
	publicURL, err := md.store.Put(ctx, key, data, mime)
	if err != nil {
		return fmt.Errorf("armazenar mídia: %w", err)
	}
	ref.Metadata["media_url"] = publicURL
	ref.Metadata["size"] = len(data)
	return nil
}

// extFromMime deriva uma extensão simples do mimetype (fallback "bin").
func extFromMime(mime string) string {
	if i := strings.Index(mime, "/"); i >= 0 && i+1 < len(mime) {
		sub := mime[i+1:]
		if j := strings.IndexAny(sub, ";+"); j >= 0 {
			sub = sub[:j]
		}
		if sub != "" {
			return sub
		}
	}
	return "bin"
}

// Compile-time assertion: we satisfy port.Sender via Adapter.
var _ port.Sender = (*Adapter)(nil)
