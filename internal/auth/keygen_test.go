package auth

import "testing"

func TestGenerateKeyAndSaltAreDistinct(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	key2, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if key1 == key2 {
		t.Fatal("generated duplicate keys")
	}

	salt1, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	salt2, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	if salt1 == salt2 {
		t.Fatal("generated duplicate salts")
	}
}

func TestHashKeyDeterministicAndSalted(t *testing.T) {
	key := "local-test-key"
	salt := "salt-one"
	hash1 := HashKey(key, salt)
	hash2 := HashKey(key, salt)
	if hash1 == "" || hash1 != hash2 {
		t.Fatalf("hash not deterministic: %q %q", hash1, hash2)
	}
	if hash1 == HashKey(key, "salt-two") {
		t.Fatal("hash did not change when salt changed")
	}
}
