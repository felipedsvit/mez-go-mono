package waba

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSendMessage_OK(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth, gotCT string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.OK"}]}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{BaseURL: srv.URL, Version: "v21.0", PhoneNumberID: "PNID", Token: "tok"})
	wamid, err := c.SendMessage(context.Background(), map[string]any{"type": "text", "to": "5511"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if wamid != "wamid.OK" {
		t.Errorf("wamid = %q, want wamid.OK", wamid)
	}
	if gotPath != "/v21.0/PNID/messages" {
		t.Errorf("path = %q, want /v21.0/PNID/messages", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q, want Bearer tok", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	if gotBody["to"] != "5511" {
		t.Errorf("body: %v", gotBody)
	}
}

func TestClientSendMessage_GraphError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad","type":"OAuthException","code":131047}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{BaseURL: srv.URL, Version: "v21.0", PhoneNumberID: "PNID", Token: "tok"})
	_, err := c.SendMessage(context.Background(), map[string]any{"type": "text"})
	if err == nil {
		t.Fatal("expected error from graph api")
	}
}

func TestClientSendMessage_NoMessageID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[]}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{BaseURL: srv.URL, Version: "v21.0", PhoneNumberID: "PNID", Token: "tok"})
	_, err := c.SendMessage(context.Background(), map[string]any{"type": "text"})
	if err == nil {
		t.Fatal("expected error when response has no message id")
	}
}

func TestClientMarkRead(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{BaseURL: srv.URL, Version: "v21.0", PhoneNumberID: "PNID", Token: "tok"})
	if err := c.MarkRead(context.Background(), "wamid.XYZ"); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if gotPath != "/v21.0/PNID/messages" {
		t.Errorf("path = %q", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotBody["status"] != "read" {
		t.Errorf("status = %v, want read", gotBody["status"])
	}
	if gotBody["message_id"] != "wamid.XYZ" {
		t.Errorf("message_id = %v", gotBody["message_id"])
	}
}

func TestClientDeleteMessage(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{BaseURL: srv.URL, Version: "v21.0", PhoneNumberID: "PNID", Token: "tok"})
	if err := c.DeleteMessage(context.Background(), "wamid.XYZ"); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if gotPath != "/v21.0/wamid.XYZ" {
		t.Errorf("path = %q, want /v21.0/wamid.XYZ", gotPath)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestClientGetMediaURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v21.0/media-123" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"url":"https://lookaside.fb.com/asset","mime_type":"image/jpeg"}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{BaseURL: srv.URL, Version: "v21.0", PhoneNumberID: "PNID", Token: "tok"})
	url, mime, err := c.GetMediaURL(context.Background(), "media-123")
	if err != nil {
		t.Fatalf("GetMediaURL: %v", err)
	}
	if url != "https://lookaside.fb.com/asset" {
		t.Errorf("url = %q", url)
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q", mime)
	}
}

func TestClientDownloadMedia(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello-media"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{BaseURL: srv.URL, Version: "v21.0", PhoneNumberID: "PNID", Token: "tok"})
	data, err := c.DownloadMedia(context.Background(), srv.URL+"/asset")
	if err != nil {
		t.Fatalf("DownloadMedia: %v", err)
	}
	if string(data) != "hello-media" {
		t.Errorf("data = %q", data)
	}
}

func TestExtFromMime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mime string
		want string
	}{
		{"image/jpeg", "jpeg"},
		{"image/png", "png"},
		{"application/pdf", "pdf"},
		{"video/mp4", "mp4"},
		{"audio/ogg; codecs=opus", "ogg"},
		{"application/octet-stream", "octet-stream"},
		{"", "bin"},
	}
	for _, tc := range cases {
		if got := extFromMime(tc.mime); got != tc.want {
			t.Errorf("extFromMime(%q) = %q, want %q", tc.mime, got, tc.want)
		}
	}
}
