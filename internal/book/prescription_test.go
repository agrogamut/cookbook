package book

import (
	"bytes"
	"context"
	"testing"
)

func TestAppendPrescriptionWithoutUploadKeepsBytes(t *testing.T) {
	pdf := []byte("%PDF-1.4\nunchanged stored document")
	got, err := AppendPrescription(context.Background(), pdf, Book1{})
	if err != nil || !bytes.Equal(got, pdf) {
		t.Fatalf("no-upload path changed bytes: %v", err)
	}
}

func TestPrescriptionMergeRejectsInvalidInput(t *testing.T) {
	if _, err := mergeBookPDF(context.Background(), []byte("not PDF"), []byte("%PDF-1.4")); err == nil {
		t.Fatal("invalid component accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mergeBookPDF(ctx, []byte("%PDF-1.4"), []byte("%PDF-1.4")); err == nil {
		t.Fatal("cancelled merge succeeded")
	}
}
