// Testes unitários que não dependem de MinIO. Validam no-op mode e parsers.

package s3

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestNew_NoEndpoint(t *testing.T) {
	s, err := New(context.Background(), zerolog.Nop(), Config{})
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Nil(t, s.client)
}

func TestNew_MissingBucket(t *testing.T) {
	_, err := New(context.Background(), zerolog.Nop(), Config{
		Endpoint: "localhost:9000",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bucket de mídia é obrigatório")
}

func TestPublicURL(t *testing.T) {
	s := &Store{publicBase: "http://localhost:9000/mez-media"}
	require.Equal(t, "http://localhost:9000/mez-media/x/y", s.PublicURL("x/y"))
}

func TestParseS3URL(t *testing.T) {
	s := &Store{}
	cases := []struct {
		raw    string
		want   string
		errMsg string
	}{
		{"http://localhost:9000/mez-media/tenants/abc/x.png", "tenants/abc/x.png", ""},
		{"https://cdn/mez-media/foo", "foo", ""},
		{"foo", "", "sem chave"},
	}
	for _, tc := range cases {
		got, err := s.ParseS3URL(tc.raw)
		if tc.errMsg != "" {
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		}
	}
}

func TestIsNotFound(t *testing.T) {
	require.True(t, isNotFound(nil) == false)
	require.True(t, isNotFound(errString("NoSuchKey: not found")))
	require.True(t, isNotFound(errString("404 Not Found")))
	require.False(t, isNotFound(errString("connection refused")))
}

type errString string

func (e errString) Error() string { return string(e) }
