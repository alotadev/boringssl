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
	zircon  = flag.String("zircon", "zircon/third_party/ulib/uboringssl", "Path to Zircon library")

	skipBoring = flag.Bool("skip-boring", false, "Don't update upstream sources or build files")
	skipRust   = flag.Bool("skip-rust", false, "Don't update Rust bindings")
	skipZircon = flag.Bool("skip-zircon", false, "Don't update Zircon's uboringssl library")
)

// These uboringssl files don't needed to be rolled from BoringSSL.
var skipped_files = map[string]bool{
	"/BUILD.gn":          true,
	"/README.fuchsia.md": true,
	"/stack-note.S":      true,
}

// Utility functions

func infof(msg string) {
	log.Printf("[+] %s\n", msg)
}

func warnf(msg string) {
	log.Printf("<!> %s\n", msg)
}

// Executes a command with the given |name| and |args| using |cwd| as the current working directory.
func run(cwd string, name string, args ...string) []byte {
	cmd := exec.Command(name, args...)
	if len(cwd) > 0 {
		cmd.Dir = cwd
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		cmdline := strings.Join(append([]string{name}, args...), " ")
		warnf("Error returned for '" + cmdline + "'")
		warnf("Output: " + string(out))
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

// Both the Zircon and third-party copies of BoringSSL have a README file that ends with the current
// upstream git revision.  This function updates those files.
func updateReadMe(readmePath string) {
	infof("  Updating README file...")

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

	infof("Updating sources...")
	run(src, "git", "fetch")
	run(src, "git", "checkout", *commit)
	*commit = string(run(src, "git", "rev-list", "HEAD", "--max-count=1"))
	*commit = strings.TrimSpace(*commit)

	updateReadMe(filepath.Join(*boring, "README.fuchsia"))

	infof("Generating build files...")
	run(*boring, "python", filepath.Join(src, "util", "generate_build_files.py"), "gn")
}

// Regenerates the Rust bindings
func updateRust() {
	run("", filepath.Join(*boring, "rust/boringssl-sys/bindgen.sh"))
}

// To update Zircon's uboringssl library, we update the revision number in the README file and
// copy any files present in uboringssl that do not match their counterpart in BoringSSL
func updateZircon() {
	updateReadMe(filepath.Join(*zircon, "README.fuchsia.md"))

	infof("  Updating sources from BoringSSL...")
	missing_files := map[string]bool{}
	walker := func(zirconPath string, zxInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if zxInfo.IsDir() {
			return nil
		}
		stem := zirconPath[len(*zircon):]
		if skipped_files[stem] {
			return nil
		}
		// Look for the matching file under boringssl or boringssl/src
		boringPath := filepath.Join(*boring, stem)
		if _, err = os.Stat(boringPath); os.IsNotExist(err) {
			boringPath = filepath.Join(*boring, "src", stem)
		}
		if _, err = os.Stat(boringPath); os.IsNotExist(err) {
			missing_files[stem] = true
		}
		// Copy files that have changed
		if sha256sum(boringPath) != sha256sum(zirconPath) {
			run(*fuchsia, "cp", boringPath, zirconPath)
		}
		return nil
	}
	if err := filepath.Walk(*zircon, walker); err != nil {
		log.Fatal(err)
	}
	// Warn about missing files
	if len(missing_files) != 0 {
		warnf("ERROR: These files are missing from upstream:")
		for file := range missing_files {
			warnf(file)
		}
		log.Fatal("Please resolve these files and try again.")
	}
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
		infof("Updating BoringSSL from upstream...")
		updateBoring()
		infof("Done!")
		infof("")
		infof("To test, please run:")
		infof("  $ fx set ... --preinstall garnet/packages/tests/boringssl")
		infof("  $ fx build")
		infof("  $ fx serve")
		infof("  $ fx run-test boringssl_tests")
		infof("If tests pass; commit the changes in " + *boring)
		infof("Then, update the BoringSSL revisions in the internal integration repository.")
	}

	if !*skipRust {
		infof("Updating Rust bindings...")
		updateRust()
		infof("Done!")
	}

	if !*skipZircon {
		infof("Updating Zircon's uboringssl library...")
		updateZircon()
		infof("Done!")
		infof("")
		infof("To test, please run launch Zircon and run:")
		infof("  > k ut prng")
		infof("  > /boot/test/sys/crypto_test")
		infof("If tests pass; commit the changes in " + *zircon)
	}

	infof("Finally, update the BoringSSL revisions in the internal integration repository.")
	infof("You can use `" + *boring + "/check-integration`to verify the revisions.")
}
