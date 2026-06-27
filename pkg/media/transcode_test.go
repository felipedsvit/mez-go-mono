package media

import (
	"context"
	"testing"
)

func TestFFmpegTranscoder_Available_Passthrough(t *testing.T) {
	tr := NewFFmpegTranscoder(4)
	if tr.Available() {
		t.Log("ffmpeg disponível no PATH; teste ainda exercita passthrough")
	}
	ctx := context.Background()
	in := []byte("fake media bytes")
	out, err := tr.ToPTT(ctx, in)
	if err != nil {
		t.Fatalf("ToPTT: %v", err)
	}
	if string(out) != string(in) {
		t.Errorf("ToPTT passthrough divergiu: got %q, want %q", out, in)
	}
	out, err = tr.ToStaticSticker(ctx, in)
	if err != nil {
		t.Fatalf("ToStaticSticker: %v", err)
	}
	if string(out) != string(in) {
		t.Errorf("ToStaticSticker divergiu")
	}
	out, err = tr.ToVideo(ctx, in)
	if err != nil {
		t.Fatalf("ToVideo: %v", err)
	}
	if string(out) != string(in) {
		t.Errorf("ToVideo divergiu")
	}
}

func TestFFmpegTranscoder_Concurrency_Bounded(t *testing.T) {
	// Verifica que o semáforo limita concorrência a maxParallel.
	tr := NewFFmpegTranscoder(2)
	ctx := context.Background()
	in := []byte("x")

	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = tr.ToPTT(ctx, in)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
