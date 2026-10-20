package main

import (
	"os"
	"testing"
)

func TestSMBTargetOptional(t *testing.T) {
	host := os.Getenv("ARCH_SMB_HOST")
	if host == "" {
		t.Skip("ARCH_SMB_HOST не задан")
	}
	c := networkConfig{Protocol: "smb2", Host: host, Path: os.Getenv("ARCH_SMB_PATH"), User: os.Getenv("ARCH_SMB_USER"), Domain: os.Getenv("ARCH_SMB_DOMAIN")}
	tg, err := dialSMB(c, os.Getenv("ARCH_SMB_PASS"))
	if err != nil {
		t.Fatal(err)
	}
	defer tg.Close()
	if err := tg.MkdirAll("arch-test"); err != nil {
		t.Fatal(err)
	}
	if err := tg.WriteAt("arch-test/probe", []byte("ok"), 0); err != nil {
		t.Fatal(err)
	}
	defer tg.Remove("arch-test/probe")
}
