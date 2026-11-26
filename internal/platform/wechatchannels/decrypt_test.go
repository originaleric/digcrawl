package wechatchannels

import (
	"bytes"
	"io"
	"testing"
)

func TestChannelsDecryptReaderTransformsOnlyEncryptedPrefix(t *testing.T) {
	original := []byte("the first encrypted prefix is reversible; the unencrypted suffix must remain exact")
	limit := uint64(37)
	encrypted, err := io.ReadAll(newChannelsDecryptReader(bytes.NewReader(original), 12345, limit))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encrypted[:limit], original[:limit]) {
		t.Fatal("encrypted prefix was not transformed")
	}
	if !bytes.Equal(encrypted[limit:], original[limit:]) {
		t.Fatal("unencrypted suffix was transformed")
	}
	decrypted, err := io.ReadAll(newChannelsDecryptReader(bytes.NewReader(encrypted), 12345, limit))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, original) {
		t.Fatalf("round trip=%q want=%q", decrypted, original)
	}
}
