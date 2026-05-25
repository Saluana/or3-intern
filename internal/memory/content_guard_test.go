package memory

import "testing"

func TestValidatePinKey(t *testing.T) {
	if err := ValidatePinKey("user_name"); err != nil {
		t.Fatalf("expected valid key: %v", err)
	}
	if err := ValidatePinKey("Bad Key"); err == nil {
		t.Fatal("expected invalid key error")
	}
}

func TestRejectSecretLikeContent(t *testing.T) {
	if err := ValidateNoteText("project uses postgres"); err != nil {
		t.Fatalf("expected benign note: %v", err)
	}
	if err := ValidateNoteText("api_key=sk-live-abcdef0123456789"); err == nil {
		t.Fatal("expected secret rejection")
	}
}
