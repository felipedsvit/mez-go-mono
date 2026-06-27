// Package whatsmeow — real_client.go: adapter de produção sobre
// *whatsmeow.Client da lib go.mau.fi/whatsmeow (tulir/whatsmeow).
//
// Issue #158 (Fase 9): substitui o stub funcional por uma implementação
// real. Mantém a forma da interface `Client` (definida em client.go) —
// drop-in replacement para `stubWhatsmeowClient`.
//
// O que difere do stub:
//
//   - Usa *store.Device persistido em Postgres via sqlstore.New(ctx, "pgx", dsn).
//   - Mídia é transduzida (FFmpeg para OGG/Opus, WebP) e uploaded via
//     client.Upload antes de construir waE2E.Message.
//   - QR code vem do canal GetQRChannel (evento QRChannelItem).
//   - Eventos whatsmeow (*events.Connected, *events.Message, etc)
//     fluem pelo EventHandler.
//
// Serialização: o *whatsmeow.Client NÃO é thread-safe (gotcha documentado
// no AGENTS.md pai). O Dispatcher per-tenant garante single-goroutine
// por tenant — todas as chamadas ao client passam por ele.
//
// Pré-requisitos:
//
//   - Postgres disponível (container: deployments/docker-compose.yml).
//   - ffmpeg/cwebp no PATH do container (Dockerfile já os instala).
//   - Variável MEZ_WHATSMEOW_DEVICE_DSN configurada.
//   - Sessão pareada (primeiro uso requer QR scan do celular).
package whatsmeow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/log"

	_ "github.com/jackc/pgx/v5/stdlib" // driver pgx para database/sql (sqlstore)
)

// MediaTranscoder é o subset do pkg/media.FFmpegTranscoder que o
// real client usa. Definido aqui como interface para desacoplar.
type MediaTranscoder interface {
	// ToPTT converte áudio (MP3/M4A/WAV) em OGG/Opus 48kHz mono.
	// Best-effort: se transcoder indisponível, devolve input como-is.
	ToPTT(ctx context.Context, in []byte) ([]byte, error)
	// ToStaticSticker converte PNG/JPEG em WebP estático.
	ToStaticSticker(ctx context.Context, in []byte) ([]byte, error)
	// Available retorna true se o ffmpeg foi encontrado no PATH.
	Available() bool
}

// RealClientConfig configura o NewRealClient.
type RealClientConfig struct {
	// DSN para o session store (sqlstore do whatsmeow). Formato padrão do pgx.
	// Se vazio, o client não consegue persistir sessão e retorna erro.
	DeviceDSN string
	// Transcoder opcional — usado para OGG/Opus e WebP.
	// Se nil, áudio/vídeo saem como bytes brutos (WhatsApp rejeita).
	Transcoder MediaTranscoder
	// Log opcional para o whatsmeow internamente (waLog). Se nil, Noop.
	WaLog waLog.Logger
}

// NewRealClient cria um *RealClient com sqlstore + device store.
//
// deviceDSN aponta para o Postgres onde o whatsmeow persiste a sessão
// (tabelas whatsmeow_*). Se o DSN estiver vazio, retorna erro
// (fail-closed) — sem DSN o canal é apenas o stub.
//
// tenantID é incluído nos logs e métricas; o session store do whatsmeow
// é por-device (JID), não por-tenant, então a separação lógica fica
// no Adapter (tenant routing) e no Manager (1 client/tenant).
func NewRealClient(ctx context.Context, tenantID string, cfg RealClientConfig, zlog zerolog.Logger) (*RealClient, error) {
	if cfg.DeviceDSN == "" {
		return nil, errors.New("whatsmeow real: DeviceDSN required (postgres para session store)")
	}

	waLogger := cfg.WaLog
	if waLogger == nil {
		waLogger = waLog.Zerolog(zlog)
	}

	// 1. Container SQL (pgx via database/sql).
	container, err := sqlstore.New(ctx, "pgx", cfg.DeviceDSN, waLogger)
	if err != nil {
		return nil, fmt.Errorf("whatsmeow real: sqlstore.New: %w", err)
	}

	// 2. Primeiro device (cria se não existir).
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsmeow real: GetFirstDevice: %w", err)
	}

	// 3. Client whatsmeow.
	cli := whatsmeow.NewClient(device, waLogger)

	return &RealClient{
		tenant:     tenantID,
		cli:        cli,
		container:  container,
		transcoder: cfg.Transcoder,
		log:        zlog.With().Str("component", "whatsmeow.real").Str("tenant", tenantID).Logger(),
	}, nil
}

// RealClient é a implementação real de `Client` sobre *whatsmeow.Client.
type RealClient struct {
	tenant     string
	cli        *whatsmeow.Client
	container  *sqlstore.Container
	transcoder MediaTranscoder
	log        zerolog.Logger
}

// Compile-time assertion: RealClient satisfaz a interface Client.
var _ Client = (*RealClient)(nil)

// Connect inicia a conexão. Se a sessão já existe, conecta direto.
// Se é primeira vez (store.ID == nil), não bloqueia — o pareamento
// acontece via GetQRChannel.
func (r *RealClient) Connect(ctx context.Context) error {
	if r.cli == nil {
		return errors.New("whatsmeow real: client is nil")
	}
	if r.cli.IsConnected() {
		return nil
	}
	if err := r.cli.Connect(); err != nil {
		return fmt.Errorf("whatsmeow real: Connect: %w", err)
	}
	return nil
}

// Disconnect encerra o socket sem remover a sessão do store.
func (r *RealClient) Disconnect() {
	if r.cli != nil {
		r.cli.Disconnect()
	}
}

// IsConnected retorna o estado atual do socket.
func (r *RealClient) IsConnected() bool {
	if r.cli == nil {
		return false
	}
	return r.cli.IsConnected()
}

// SendMessage envia texto. Mapeia erros whatsmeow para os sentinelas da
// interface (ErrNotConnected).
func (r *RealClient) SendMessage(ctx context.Context, to types.JID, text string) (string, error) {
	if !r.IsConnected() {
		return "", ErrNotConnected
	}
	resp, err := r.cli.SendMessage(ctx, to, &waE2E.Message{
		Conversation: proto.String(text),
	})
	if err != nil {
		if errors.Is(err, whatsmeow.ErrNotConnected) {
			return "", ErrNotConnected
		}
		return "", fmt.Errorf("whatsmeow real: SendMessage: %w", err)
	}
	return resp.ID, nil
}

// SendImage envia imagem. Se transcoder ausente e mimetype não-WebP, falha
// de forma clara (a mono decide se aceita ou rejeita).
func (r *RealClient) SendImage(ctx context.Context, to types.JID, data []byte, mime, caption string) (string, error) {
	if !r.IsConnected() {
		return "", ErrNotConnected
	}
	// Converte para WebP estático se transcoder disponível.
	if r.transcoder != nil && r.transcoder.Available() {
		var err error
		data, err = r.transcoder.ToStaticSticker(ctx, data)
		if err != nil {
			r.log.Warn().Err(err).Msg("transcoder ToStaticSticker falhou, enviando bytes brutos")
		} else {
			mime = "image/webp"
		}
	}
	upload, err := r.cli.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: Upload image: %w", err)
	}
	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			URL:           proto.String(upload.URL),
			DirectPath:    proto.String(upload.DirectPath),
			MediaKey:      upload.MediaKey,
			FileEncSHA256: upload.FileEncSHA256,
			FileSHA256:    upload.FileSHA256,
			FileLength:    proto.Uint64(upload.FileLength),
			Mimetype:      proto.String(mime),
			Caption:       proto.String(caption),
		},
	}
	resp, err := r.cli.SendMessage(ctx, to, msg)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: SendMessage image: %w", err)
	}
	return resp.ID, nil
}

// SendAudio envia áudio. Se ptt=true, transduz para OGG/Opus.
func (r *RealClient) SendAudio(ctx context.Context, to types.JID, data []byte, mime string, ptt bool) (string, error) {
	if !r.IsConnected() {
		return "", ErrNotConnected
	}
	if ptt && r.transcoder != nil && r.transcoder.Available() {
		var err error
		data, err = r.transcoder.ToPTT(ctx, data)
		if err != nil {
			r.log.Warn().Err(err).Msg("transcoder ToPTT falhou, enviando bytes brutos")
		} else {
			mime = "audio/ogg; codecs=opus"
		}
	}
	upload, err := r.cli.Upload(ctx, data, whatsmeow.MediaAudio)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: Upload audio: %w", err)
	}
	msg := &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{
			URL:           proto.String(upload.URL),
			DirectPath:    proto.String(upload.DirectPath),
			MediaKey:      upload.MediaKey,
			FileEncSHA256: upload.FileEncSHA256,
			FileSHA256:    upload.FileSHA256,
			FileLength:    proto.Uint64(upload.FileLength),
			Mimetype:      proto.String(mime),
			PTT:           proto.Bool(ptt),
		},
	}
	resp, err := r.cli.SendMessage(ctx, to, msg)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: SendMessage audio: %w", err)
	}
	return resp.ID, nil
}

// SendVideo envia vídeo.
func (r *RealClient) SendVideo(ctx context.Context, to types.JID, data []byte, mime, caption string) (string, error) {
	if !r.IsConnected() {
		return "", ErrNotConnected
	}
	upload, err := r.cli.Upload(ctx, data, whatsmeow.MediaVideo)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: Upload video: %w", err)
	}
	msg := &waE2E.Message{
		VideoMessage: &waE2E.VideoMessage{
			URL:           proto.String(upload.URL),
			DirectPath:    proto.String(upload.DirectPath),
			MediaKey:      upload.MediaKey,
			FileEncSHA256: upload.FileEncSHA256,
			FileSHA256:    upload.FileSHA256,
			FileLength:    proto.Uint64(upload.FileLength),
			Mimetype:      proto.String(mime),
			Caption:       proto.String(caption),
		},
	}
	resp, err := r.cli.SendMessage(ctx, to, msg)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: SendMessage video: %w", err)
	}
	return resp.ID, nil
}

// SendDocument envia documento (filename é o nome no WhatsApp).
func (r *RealClient) SendDocument(ctx context.Context, to types.JID, data []byte, mime, filename, caption string) (string, error) {
	if !r.IsConnected() {
		return "", ErrNotConnected
	}
	upload, err := r.cli.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: Upload document: %w", err)
	}
	msg := &waE2E.Message{
		DocumentMessage: &waE2E.DocumentMessage{
			URL:           proto.String(upload.URL),
			DirectPath:    proto.String(upload.DirectPath),
			MediaKey:      upload.MediaKey,
			FileEncSHA256: upload.FileEncSHA256,
			FileSHA256:    upload.FileSHA256,
			FileLength:    proto.Uint64(upload.FileLength),
			Mimetype:      proto.String(mime),
			FileName:      proto.String(filename),
			Title:         proto.String(caption),
		},
	}
	resp, err := r.cli.SendMessage(ctx, to, msg)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: SendMessage document: %w", err)
	}
	return resp.ID, nil
}

// SendSticker envia sticker estático (WebP).
func (r *RealClient) SendSticker(ctx context.Context, to types.JID, data []byte) (string, error) {
	if !r.IsConnected() {
		return "", ErrNotConnected
	}
	if r.transcoder != nil && r.transcoder.Available() {
		var err error
		data, err = r.transcoder.ToStaticSticker(ctx, data)
		if err != nil {
			r.log.Warn().Err(err).Msg("transcoder ToStaticSticker falhou, enviando bytes brutos")
		}
	}
	upload, err := r.cli.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: Upload sticker: %w", err)
	}
	msg := &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{
			URL:           proto.String(upload.URL),
			DirectPath:    proto.String(upload.DirectPath),
			MediaKey:      upload.MediaKey,
			FileEncSHA256: upload.FileEncSHA256,
			FileSHA256:    upload.FileSHA256,
			FileLength:    proto.Uint64(upload.FileLength),
			Mimetype:      proto.String("image/webp"),
			IsAnimated:    proto.Bool(false),
		},
	}
	resp, err := r.cli.SendMessage(ctx, to, msg)
	if err != nil {
		return "", fmt.Errorf("whatsmeow real: SendMessage sticker: %w", err)
	}
	return resp.ID, nil
}

// SendReaction adiciona/remove uma reação. Para remover, passar emoji vazio.
func (r *RealClient) SendReaction(ctx context.Context, chat types.JID, msgID types.MessageID, emoji string) error {
	if !r.IsConnected() {
		return ErrNotConnected
	}
	// O whatsmeow precisa do sender JID para construir a reação.
	// O Adapter passa o nosso JID via chat (assumimos que chat é o peer);
	// para a construção interna, usamos o próprio chat como sender.
	msg := r.cli.BuildReaction(chat, chat, msgID, emoji)
	if _, err := r.cli.SendMessage(ctx, chat, msg); err != nil {
		return fmt.Errorf("whatsmeow real: SendReaction: %w", err)
	}
	return nil
}

// EditMessage edita uma mensagem enviada (whatsmeow só permite editar
// mensagens próprias dentro da janela de 20min — constante EditWindow).
func (r *RealClient) EditMessage(ctx context.Context, chat types.JID, msgID types.MessageID, newText string) (bool, error) {
	if !r.IsConnected() {
		return false, ErrNotConnected
	}
	newMsg := &waE2E.Message{Conversation: proto.String(newText)}
	msg := r.cli.BuildEdit(chat, msgID, newMsg)
	_, err := r.cli.SendMessage(ctx, chat, msg)
	if err != nil {
		return false, fmt.Errorf("whatsmeow real: EditMessage: %w", err)
	}
	return true, nil
}

// RevokeMessage revoga (deleta) uma mensagem enviada.
func (r *RealClient) RevokeMessage(ctx context.Context, chat types.JID, msgID types.MessageID) (bool, error) {
	if !r.IsConnected() {
		return false, ErrNotConnected
	}
	msg := r.cli.BuildRevoke(chat, chat, msgID)
	_, err := r.cli.SendMessage(ctx, chat, msg)
	if err != nil {
		return false, fmt.Errorf("whatsmeow real: RevokeMessage: %w", err)
	}
	return true, nil
}

// MarkRead marca mensagens como lidas.
func (r *RealClient) MarkRead(ctx context.Context, chat types.JID, msgIDs []types.MessageID, timestamp int64) error {
	if !r.IsConnected() {
		return ErrNotConnected
	}
	ts := time.Now()
	if timestamp > 0 {
		ts = time.Unix(timestamp, 0)
	}
	if err := r.cli.MarkRead(ctx, msgIDs, ts, chat, chat, types.ReceiptTypeRead); err != nil {
		return fmt.Errorf("whatsmeow real: MarkRead: %w", err)
	}
	return nil
}

// SendChatPresence envia typing/paused (composing/paused).
func (r *RealClient) SendChatPresence(ctx context.Context, chat types.JID, state types.ChatPresence) error {
	if !r.IsConnected() {
		return ErrNotConnected
	}
	if err := r.cli.SendChatPresence(ctx, chat, state, types.ChatPresenceMediaText); err != nil {
		return fmt.Errorf("whatsmeow real: SendChatPresence: %w", err)
	}
	return nil
}

// SendPresence envia presence (available/unavailable).
func (r *RealClient) SendPresence(ctx context.Context, state types.Presence) error {
	if !r.IsConnected() {
		return ErrNotConnected
	}
	if err := r.cli.SendPresence(ctx, state); err != nil {
		return fmt.Errorf("whatsmeow real: SendPresence: %w", err)
	}
	return nil
}

// RejectCall rejeita uma chamada recebida.
func (r *RealClient) RejectCall(ctx context.Context, from types.JID, callID string) error {
	if !r.IsConnected() {
		return ErrNotConnected
	}
	if err := r.cli.RejectCall(ctx, from, callID); err != nil {
		return fmt.Errorf("whatsmeow real: RejectCall: %w", err)
	}
	return nil
}

// GetQRChannel consome o canal de pareamento e converte para QRCodeEvent.
// Se o client já está conectado ou tem ID no store, retorna (nil, nil).
func (r *RealClient) GetQRChannel(ctx context.Context) (<-chan QRCodeEvent, error) {
	if r.cli == nil {
		return nil, errors.New("whatsmeow real: client is nil")
	}
	if r.cli.IsConnected() {
		return nil, nil // já conectado
	}
	if r.cli.Store.ID != nil {
		return nil, nil // já tem ID, Connect direto (sem QR)
	}
	waCh, err := r.cli.GetQRChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsmeow real: GetQRChannel: %w", err)
	}

	// Converte de whatsmeow.QRChannelItem para nossa QRCodeEvent.
	out := make(chan QRCodeEvent, 8)
	go func() {
		defer close(out)
		for item := range waCh {
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				select {
				case out <- QRCodeEvent{Code: item.Code, Event: "code"}:
				case <-ctx.Done():
					return
				}
			case "success":
				select {
				case out <- QRCodeEvent{Event: "success"}:
				case <-ctx.Done():
					return
				}
			case "timeout":
				select {
				case out <- QRCodeEvent{Event: "timeout"}:
				case <-ctx.Done():
					return
				}
			case "err-unexpected-state":
				select {
				case out <- QRCodeEvent{Event: "err-unexpected-state"}:
				case <-ctx.Done():
					return
				}
			case "err-client-outdated":
				select {
				case out <- QRCodeEvent{Event: "err-client-outdated"}:
				case <-ctx.Done():
					return
				}
			case "err-scanned-without-multidevice":
				select {
				case out <- QRCodeEvent{Event: "err-scanned-without-multidevice"}:
				case <-ctx.Done():
					return
				}
			case whatsmeow.QRChannelEventError:
				select {
				case out <- QRCodeEvent{Event: "error"}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// AddEventHandler registra o handler de eventos. A interface recebe `any`
// para desacoplar; o handler converte para *events.X conforme o tipo.
func (r *RealClient) AddEventHandler(handler EventHandler) uint32 {
	if r.cli == nil {
		return 0
	}
	// Wrap: o handler do client espera func(any); nossa interface
	// também é func(any). Wrapping é identity mas permite logging.
	wrapped := func(evt any) {
		r.log.Debug().
			Str("type", fmt.Sprintf("%T", evt)).
			Msg("whatsmeow real: event received")
		handler(evt)
	}
	return r.cli.AddEventHandler(wrapped)
}

// Logout encerra a sessão e remove o device do store.
func (r *RealClient) Logout(ctx context.Context) error {
	if r.cli == nil {
		return nil
	}
	if err := r.cli.Logout(ctx); err != nil {
		return fmt.Errorf("whatsmeow real: Logout: %w", err)
	}
	return nil
}

// IsLoggedIn retorna true se o client tem sessão persistida.
// Útil para o Manager decidir se precisa de QR ou Connect direto.
func (r *RealClient) IsLoggedIn() bool {
	if r.cli == nil {
		return false
	}
	return r.cli.Store.ID != nil
}

// Container devolve o *sqlstore.Container (para testes/admin).
func (r *RealClient) Container() *sqlstore.Container {
	return r.container
}

// --- Helper: tipo de evento para logs ---

// EventType retorna uma string curta para o tipo de evento whatsmeow.
// Útil para logging sem importar events em outros packages.
func EventType(evt any) string {
	switch evt.(type) {
	case *events.Connected:
		return "connected"
	case *events.Disconnected:
		return "disconnected"
	case *events.Message:
		return "message"
	case *events.Receipt:
		return "receipt"
	case *events.PairSuccess:
		return "pair_success"
	case *events.PairError:
		return "pair_error"
	case *events.HistorySync:
		return "history_sync"
	case *events.QR:
		return "qr"
	case *events.GroupInfo:
		return "group_info"
	case *events.JoinedGroup:
		return "joined_group"
	case *events.CallOffer:
		return "call_offer"
	case *events.LoggedOut:
		return "logged_out"
	default:
		return fmt.Sprintf("%T", evt)
	}
}
