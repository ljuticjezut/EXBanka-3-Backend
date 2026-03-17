package models_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RAF-SI-2025/EXBanka-3-Backend/account-service/internal/models"
)

func TestFirma_RequiredFields(t *testing.T) {
	rt := reflect.TypeOf(models.Firma{})

	required := []string{"Naziv", "MaticniBroj", "PIB"}
	for _, fieldName := range required {
		f, ok := rt.FieldByName(fieldName)
		if !ok {
			t.Fatalf("field %s not found on Firma", fieldName)
		}
		tag := f.Tag.Get("gorm")
		if !strings.Contains(tag, "not null") {
			t.Errorf("Firma.%s: expected gorm tag to contain 'not null', got: %s", fieldName, tag)
		}
	}
}

func TestFirma_UniqueIndexes(t *testing.T) {
	rt := reflect.TypeOf(models.Firma{})

	for _, fieldName := range []string{"MaticniBroj", "PIB"} {
		f, ok := rt.FieldByName(fieldName)
		if !ok {
			t.Fatalf("field %s not found on Firma", fieldName)
		}
		tag := f.Tag.Get("gorm")
		if !strings.Contains(tag, "uniqueIndex") {
			t.Errorf("Firma.%s: expected gorm uniqueIndex, got: %s", fieldName, tag)
		}
	}
}

func TestSifraDelatnosti_HasSifraAndNaziv(t *testing.T) {
	rt := reflect.TypeOf(models.SifraDelatnosti{})

	for _, fieldName := range []string{"Sifra", "Naziv"} {
		f, ok := rt.FieldByName(fieldName)
		if !ok {
			t.Fatalf("field %s not found on SifraDelatnosti", fieldName)
		}
		tag := f.Tag.Get("gorm")
		if !strings.Contains(tag, "not null") {
			t.Errorf("SifraDelatnosti.%s: expected 'not null' gorm tag, got: %s", fieldName, tag)
		}
	}

	s := models.SifraDelatnosti{Sifra: "6419", Naziv: "Monetarne institucije"}
	if s.Sifra != "6419" {
		t.Errorf("expected Sifra=6419, got %s", s.Sifra)
	}
	if s.Naziv == "" {
		t.Error("expected Naziv to be set")
	}
}
