package sshd

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestModernHostSignerDropsSHA1(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	restricted, ok := modernHostSigner(signer).(gossh.MultiAlgorithmSigner)
	if !ok {
		t.Fatal("RSA host key must be restricted to an explicit algorithm list")
	}
	for _, algo := range restricted.Algorithms() {
		if algo == gossh.KeyAlgoRSA {
			t.Fatal("ssh-rsa (SHA-1) must not be offered")
		}
	}
	if len(restricted.Algorithms()) != 2 {
		t.Fatalf("unexpected algorithms: %v", restricted.Algorithms())
	}
}

func TestDefaultSSHSrvConfigHasNoSHA1(t *testing.T) {
	cfg := DefaultSSHSrvConfig()
	for _, mac := range cfg.MACs {
		if mac == gossh.HMACSHA1 || mac == gossh.InsecureHMACSHA196 {
			t.Fatalf("SHA-1 MAC offered: %s", mac)
		}
	}
	if len(cfg.MACs) == 0 || len(cfg.KeyExchanges) == 0 {
		t.Fatal("SSH config must keep at least one MAC and key exchange")
	}
	for _, kex := range cfg.KeyExchanges {
		if kex == gossh.InsecureKeyExchangeDH1SHA1 || kex == gossh.InsecureKeyExchangeDH14SHA1 {
			t.Fatalf("SHA-1 key exchange offered: %s", kex)
		}
	}
}
