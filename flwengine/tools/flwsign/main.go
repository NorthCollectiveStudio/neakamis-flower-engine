package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

func main() {
	in := flag.String("in", "", "input binary")
	out := flag.String("out", "", "output binary (default overwrite input)")
	seedFile := flag.String("signkey", "", "signing seed file (base64 seed)")
	verify := flag.Bool("verify", false, "verify instead of sign")
	pubFile := flag.String("pub", "", "public key file for verify")
	flag.Parse()
	if *in == "" {
		fmt.Fprintln(os.Stderr, "usage: flwsign -in <bin> [-out <bin>] -signkey <seed> | -verify -pub <pub>")
		os.Exit(2)
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read failed:", err)
		os.Exit(1)
	}
	if *verify {
		raw, err := os.ReadFile(*pubFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pub read failed:", err)
			os.Exit(1)
		}
		pub, err := base64.StdEncoding.DecodeString(string(raw))
		if err != nil || len(pub) != ed25519.PublicKeySize {
			fmt.Fprintln(os.Stderr, "bad public key")
			os.Exit(1)
		}
		if len(data) < 75 || string(data[len(data)-75:len(data)-68]) != "FLWSIG1" {
			fmt.Fprintln(os.Stderr, "no trailer")
			os.Exit(1)
		}
		sigLen := binary.LittleEndian.Uint32(data[len(data)-68 : len(data)-64])
		if sigLen != 64 {
			fmt.Fprintln(os.Stderr, "bad trailer")
			os.Exit(1)
		}
		sig := data[len(data)-64:]
		body := data[:len(data)-75]
		digest := sha256.Sum256(body)
		if !ed25519.Verify(ed25519.PublicKey(pub), digest[:], sig) {
			fmt.Fprintln(os.Stderr, "signature invalid")
			os.Exit(1)
		}
		fmt.Println("signature ok")
		return
	}
	seedRaw, err := os.ReadFile(*seedFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed read failed:", err)
		os.Exit(1)
	}
	seed, err := base64.StdEncoding.DecodeString(string(seedRaw))
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintln(os.Stderr, "bad seed")
		os.Exit(1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	body := data
	if len(data) >= 75 && string(data[len(data)-75:len(data)-68]) == "FLWSIG1" {
		body = data[:len(data)-75]
	}
	digest := sha256.Sum256(body)
	sig := ed25519.Sign(priv, digest[:])
	outBytes := make([]byte, 0, len(body)+75)
	outBytes = append(outBytes, body...)
	outBytes = append(outBytes, []byte("FLWSIG1")...)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(sig)))
	outBytes = append(outBytes, lenBuf[:]...)
	outBytes = append(outBytes, sig...)
	dst := *out
	if dst == "" {
		dst = *in
	}
	if err := os.WriteFile(dst, outBytes, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "write failed:", err)
		os.Exit(1)
	}
	fmt.Printf("signed %s (%d bytes)\n", dst, len(outBytes))
}
