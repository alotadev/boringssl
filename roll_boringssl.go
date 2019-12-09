// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.
//
// This script updates //third_party/boringssl/src to point to the current revision at:
//   https://boringssl.googlesource.com/boringssl/+/master
//
// It also updates the generated build files, Rust bindings, and subset of code used in Zircon.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	boring  = flag.String("boring", "third_party/boringssl", "Path to repository")
	commit  = flag.String("commit", "origin/upstream/master", "Upstream commit-ish to check out")
	fuchsia = flag.String("fuchsia", os.Getenv("FUCHSIA_DIR"), "Fuchsia root directory")

	skipBoring = flag.Bool("skip-boring", false, "Don't update upstream sources or build files")
	skipRust   = flag.Bool("skip-rust", false, "Don't update Rust bindings")
)

// Executes a command with the given |name| and |args| using |cwd| as the current working directory.
func run(cwd string, name string, args ...string) []byte {
	cmd := exec.Command(name, args...)
	if len(cwd) > 0 {
		cmd.Dir = cwd
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		cmdline := strings.Join(append([]string{name}, args...), " ")
		log.Printf("Error returned for '%s'.\n", cmdline)
		log.Printf("Output: %s\n", string(out))
		log.Fatal(err)
	}
	return out
}

// Sha256Sum returns the hex-encoded SHA256 digest of a file
func sha256sum(path string) string {
	file, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// Updates the README.fuchsia.md file that ends with the current upstream git revision.
func updateReadMe(readmePath string) {
	log.Printf("  Updating README file...\n")

	// Open the README.fuchsia file
	readme, err := os.OpenFile(readmePath, os.O_RDWR, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer readme.Close()

	// Check that the file ends with a git URL
	const urlbase string = "https://fuchsia.googlesource.com/third_party/boringssl/+/"
	urllen := int64(len(urlbase))
	url := make([]byte, urllen)
	off := int64(0)
	var bytes_read int
	info, err := readme.Stat()
	if err != nil {
		log.Fatal(err)
	}
	revlen := int64(len(*commit)) + 1
	if info.Size() > urllen+revlen {
		off = info.Size() - (urllen + revlen + 1)
	}
	if bytes_read, err = readme.ReadAt(url, off); err != nil {
		log.Fatal(err)
	}
	if int64(bytes_read) < urllen || urlbase != string(url) {
		log.Fatal(readmePath + " does not end with a valid git URL")
	}

	// Write the new git revision into the URL
	off += urllen
	if _, err = readme.WriteAt([]byte(*commit + "/"), off); err != nil {
		log.Fatal(err)
	}
}

// Pulls latest BoringSSL for upstream, and generates new build files
func updateBoring() {
	src := filepath.Join(*boring, "src")

	log.Printf("Updating sources...\n")
	run(src, "git", "fetch")
	run(src, "git", "checkout", *commit)
	*commit = string(run(src, "git", "rev-list", "HEAD", "--max-count=1"))
	*commit = strings.TrimSpace(*commit)

	updateReadMe(filepath.Join(*boring, "README.fuchsia"))

	log.Printf("Generating build files...\n")
	run(*boring, "python", filepath.Join(src, "util", "generate_build_files.py"), "gn")
}

// Regenerates the Rust bindings
func updateRust() {
	run("", filepath.Join(*boring, "rust/boringssl-sys/bindgen.sh"))
}

// Main function
func main() {
	flag.Parse()
	if len(*fuchsia) == 0 {
		log.Fatal(errors.New("FUCHSIA_DIR not set and --fuchsia not specified"))
	}
	*boring = filepath.Join(*fuchsia, *boring)
	*zircon = filepath.Join(*fuchsia, *zircon)

	if !*skipBoring {
		log.Printf("Updating BoringSSL from upstream...\n")
		updateBoring()
		log.Printf("Done!\n")
	}
	if !*skipRust {
		log.Printf("Updating Rust bindings...\n")
		updateRust()
		log.Printf("Done!\n")
	}

	log.Printf("\n")
	log.Printf("To test, please run:\n")
	log.Printf("  $ fx set ... --with //third_party/boringssl:boringssl_tests\n")
	log.Printf("  $ fx build\n")
	log.Printf("  $ fx serve\n")
	log.Printf("  $ fx run-test boringssl_tests\n")

	log.Printf("If tests pass; commit the changes in %s.\n", *boring)
	log.Printf("Then, update the BoringSSL revisions in the internal integration repository.\n")
}
