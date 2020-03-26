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
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	commit = flag.String("commit", "origin/upstream/master", "Upstream commit-ish to check out")
)

// Executes a command with the given |name| and |args|.
func run(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		cmdline := strings.Join(append([]string{name}, args...), " ")
		log.Printf("Error returned for '%s'.\n", cmdline)
		log.Printf("Output: %s\n", string(out))
		log.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// Changes to the BoringSSL directory and resolves the git commit to a SHA-1.
func configure() {
	log.Printf("Configuring...\n")
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("Failed to find current executable.\n")
	}
	os.Chdir(filepath.Dir(file))

	flag.Parse()
	*commit = run("git", "rev-parse", *commit)
	log.Printf("Commit resolved to %s\n", *commit)
}

// Copies sources from resolved commit to the working tree.
func updateSources() {
	log.Printf("Updating BoringSSL sources...\n")
	var err error
	if err = os.RemoveAll("src"); err != nil {
		log.Fatalf("Failed to remove directory 'src': %v\n", err)
	}
	if err = os.Mkdir("src", 0700); err != nil {
		log.Fatalf("Failed to make directory 'src': %v\n", err)
	}
	cmd1 := exec.Command("git", "archive", *commit, "--worktree-attributes")
	cmd2 := exec.Command("tar", "xC", "src")
	if cmd2.Stdin, err = cmd1.StdoutPipe(); err != nil {
		log.Fatalf("Failed to create pipe from 'git': %v\n", err)
	}
	if err = cmd2.Start(); err != nil {
		log.Fatalf("Failed to start 'tar': %v\n", err)
	}
	if err = cmd1.Run(); err != nil {
		log.Fatalf("'git' returned error: %v\n", err)
	}
	if err = cmd2.Wait(); err != nil {
		log.Fatalf("'tar' returned error: %v\n", err)
	}
}

// Create the GN build files for the current sources.
func generateGN() {
	log.Printf("Generating build files...\n")
	run("python", filepath.Join("src", "util", "generate_build_files.py"), "gn")
}

// Regenerates the Rust bindings
func generateRustBindings() {
	log.Printf("Generating Rust bindings...\n")
	run(filepath.Join("rust", "boringssl-sys", "bindgen.sh"))
}

// Updates the README.md file that ends with the current upstream git revision.
func updateReadMe() {
	log.Printf("Updating README file...\n")
	readme, err := os.OpenFile("README.fuchsia.md", os.O_RDWR, 0644)
	if err != nil {
		log.Fatalf("Failed to open README.md: %v\n", err)
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
		log.Fatalf("Failed to stat README.fuchsia.md: %v\n", err)
	}
	revlen := int64(len(*commit)) + 1
	if info.Size() > urllen+revlen {
		off = info.Size() - (urllen + revlen + 1)
	}
	if bytes_read, err = readme.ReadAt(url, off); err != nil {
		log.Fatalf("Failed to read from README.fuchsia.md: %v\n", err)
	}
	if int64(bytes_read) < urllen || urlbase != string(url) {
		log.Fatal("README.fuchsia.md does not end with a valid git URL.\n")
	}
	off += urllen
	if _, err = readme.WriteAt([]byte(*commit+"/"), off); err != nil {
		log.Fatalf("Failed to write to README.fuchsia.md: %v\n", err)
	}
}

// Main function
func main() {
	configure()
	updateSources()
	generateGN()
	generateRustBindings()
	updateReadMe()

	log.Printf("\n")
	log.Printf("To test, please run:\n")
	log.Printf("  $ fx set ... --with //third_party/boringssl:tests\n")
	log.Printf("  $ fx build\n")
	log.Printf("  $ fx serve\n")
	log.Printf("  $ fx run-test boringssl_tests\n")

	log.Printf("If tests pass; commit the changes in //third_party/boringssl.\n")
	log.Printf("Then, update the BoringSSL revision in the internal integration repository.\n")
}
