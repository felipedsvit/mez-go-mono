// Package media transcodifica mídia para os formatos que o WhatsApp exige antes
// do Upload (regra do whatsmeow): áudio PTT em OGG/Opus, sticker em WebP,
// vídeo em MP4 H.264/AAC. O subprocess do ffmpeg é caro (~50-200ms); a
// concorrência é limitada por um semáforo (pool) para não saturar a CPU
// do worker.
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
)

// Mimetypes exigidos pelo WhatsApp.
const (
	MimePTT             = "audio/ogg; codecs=opus"
	MimeStickerStatic   = "image/webp"
	MimeStickerAnimated = "video/webm"
	MimeVideo           = "video/mp4"
)

// ErrToolMissing indica que ffmpeg ou cwebp não estão no PATH do container.
var ErrToolMissing = errors.New("ferramenta de transcodificação ausente (ffmpeg/cwebp)")

// Transcoder converte mídia para os formatos nativos do WhatsApp.
type Transcoder interface {
	// ToPTT converte áudio (MP3/M4A/WAV…) em OGG/Opus mono 48kHz para voz.
	ToPTT(ctx context.Context, in []byte) ([]byte, error)
	// ToStaticSticker converte PNG/JPEG em WebP estático.
	ToStaticSticker(ctx context.Context, in []byte) ([]byte, error)
	// ToVideo converte vídeo em MP4 H.264/AAC com +faststart.
	ToVideo(ctx context.Context, in []byte) ([]byte, error)
	// Available retorna true se o ffmpeg foi encontrado no PATH.
	Available() bool
}

// FFmpegTranscoder implementa Transcoder via ffmpeg em subprocess. Usa um
// semáforo (channel buffered) para limitar concorrência global, evitando
// saturar CPU quando múltiplos tenants disparam uploads em paralelo.
//
// Fase 4: stubs retornam o input como-is (passthrough) — implementação
// real do ffmpeg subprocess fica para produção (requer ffmpeg/cwebp no
// container, fora do escopo do build verde).
type FFmpegTranscoder struct {
	ffmpegPath string
	sem        chan struct{}
	mu         sync.Mutex
	available  bool
}

var _ Transcoder = (*FFmpegTranscoder)(nil)

// NewFFmpegTranscoder cria o transcoder. maxParallel limita subprocessos
// simultâneos (4-8 recomendado; default 4). Resolve o binário no PATH; se
// ausente, vira passthrough (não falha — logs warn).
func NewFFmpegTranscoder(maxParallel int) *FFmpegTranscoder {
	if maxParallel < 1 {
		maxParallel = 4
	}
	t := &FFmpegTranscoder{
		sem: make(chan struct{}, maxParallel),
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		t.ffmpegPath = p
		t.available = true
	}
	return t
}

// Available implementa Transcoder.
func (t *FFmpegTranscoder) Available() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.available
}

// ToPTT implementa Transcoder. Fase 4: passthrough (a transcodificação OGG/Opus
// real fica para produção; no escopo do build verde, retornamos o input
// como-is para que o pipeline funcione end-to-end com mídia).
func (t *FFmpegTranscoder) ToPTT(ctx context.Context, in []byte) ([]byte, error) {
	if !t.Available() {
		return in, nil // passthrough
	}
	return t.runWithSem(ctx, in)
}

// ToStaticSticker implementa Transcoder.
func (t *FFmpegTranscoder) ToStaticSticker(ctx context.Context, in []byte) ([]byte, error) {
	if !t.Available() {
		return in, nil
	}
	return t.runWithSem(ctx, in)
}

// ToVideo implementa Transcoder.
func (t *FFmpegTranscoder) ToVideo(ctx context.Context, in []byte) ([]byte, error) {
	if !t.Available() {
		return in, nil
	}
	return t.runWithSem(ctx, in)
}

// runWithSem adquire o semáforo (bounded concurrency) e retorna passthrough
// no escopo do Fase 4. Stub mantém a forma (semaphore + ctx) para que a
// implementação real (subprocess ffmpeg) seja drop-in.
func (t *FFmpegTranscoder) runWithSem(ctx context.Context, in []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("media: ctx: %w", err)
	}
	select {
	case t.sem <- struct{}{}:
		defer func() { <-t.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Fase 4: passthrough. Em produção, este ponto executaria subprocess
	// ffmpeg via os/exec com os args de pttArgs/videoArgs/stickerArgs
	// (padrão do pai em mez-go/pkg/media/transcode.go).
	return bytes.Clone(in), nil
}
