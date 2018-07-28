// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.
//
// This script updates //third_party/boringssl/src to point to the current revision at:
//   https://boringssl.googlesource.com/boringssl/+/master
//
// It then updates the generated build files and jiri manifest accordingly. It can optionally also
// update the root certificates used by BoringSSL on Fuchsia.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	boring  = flag.String("boring", "third_party/boringssl", "Path to repository")
	commit  = flag.String("commit", "origin/upstream/master", "Upstream commit-ish to check out")
	fuchsia = flag.String("fuchsia", os.Getenv("FUCHSIA_DIR"), "Fuchsia root directory")
	garnet  = flag.String("garnet", "garnet/manifest", "Path to Garnet manifest directoy")
	zircon  = flag.String("zircon", "zircon/third_party/ulib/uboringssl", "Path to Zircon library")

	skipFuchsia = flag.Bool("skip-fuchsia", false, "Don't run 'jiri update' first")
	skipBoring  = flag.Bool("skip-boring", false, "Don't update upstream sources or build files")
	skipRust    = flag.Bool("skip-rust", false, "Don't update Rust bindings")
	skipZircon  = flag.Bool("skip-zircon", false, "Don't update Zircon's uboringssl library")
	skipGarnet  = flag.Bool("skip-garnet", false, "Don't update Garnet's third_party manifest")

	reset  = flag.Bool("reset", false, "Reset repositories to JIRI_HEAD; ignores all other flags")
	submit = flag.Bool("submit", false, "Submits new topic to gerrit; ignores all other flags")
)

// These uboringssl files don't needed to be rolled from BoringSSL.
var skipped_files = map[string]bool{
	"/README.fuchsia.md": true,
	"/rules.mk":          true,
	"/stack-note.S":      true,
}

// These files have manual edits.  The hex string is the SHA256 digest of the original file; it will
// be flagged as having changed if the digest doesn't match.
var edited_files = map[string]string{
	"/include/openssl/base.h": "f7334f90a17f2dccded5d9d361784dbf8291a60ad4c04147a06b2f59e0e49d51",
}

// This variable will be populated with files needing manual intervention, either because they are
// edited in both uboringssl and BoringSSL, or because they exist in the former but not the latter.
var manual_files = map[string]bool{}

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

// Throws away changes in a repo and resets it to JIRI_HEAD
func resetRepo(repoPath string) {
	run(repoPath, "git", "reset", "--hard")
	run(repoPath, "git", "checkout", "JIRI_HEAD")
}

// Returns the current git revision as a SHA-1 digest.
func getGitRevision(repoPath string) []byte {
	return run(repoPath, "git", "rev-list", "HEAD", "--max-count=1")
}

// |updateManifest| uses 'jiri edit' to find a project or import (as indicated in |elemType|) with a
// name matching the given |repoPath|, and updates it to match its current revision.
func updateManifest(elemType string, repoPath string, manifest string) {
	relpath, err := filepath.Rel(*fuchsia, repoPath)
	if err != nil {
		log.Fatal(err)
	}
	rev := strings.TrimSpace(string(getGitRevision(repoPath)))
	run(*fuchsia, "jiri", "edit", "-"+elemType+"="+relpath+"="+rev, manifest)
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

// Adds all changes in the |repo| and commits them labeled by the triggering |revision|.
func commitChanges(repoPath string) {
	infof("  Committing changes...")
	rev := getGitRevision(filepath.Join(*boring, "src"))
	out := run(repoPath, "git", "status", "--short")
	if len(out) == 0 {
		return
	}
	run(repoPath, "git", "add", ".")
	run(repoPath, "git", "commit", "-m", "[boringssl] Roll to "+string(rev[:10]))
}

// Pushes a commit to a review with the given topic.
func submitTopic(repoPath, topic string) {
	run(repoPath, "git", "push", "origin", "HEAD:refs/for/master", "-o", "topic="+topic)
}

// Top-level function to update each portion of the roll
func updateFuchsia() {
	infof("  Checking Jiri status...")
	out := run(*fuchsia, "jiri", "status")
	if len(out) != 0 {
		warnf("'jiri status' returned results:")
		warnf(string(out))
		log.Fatal("Please ensure all projects are on JIRI_HEAD and clean before trying again.")
	}

	infof("  Updating via Jiri...")
	run(*fuchsia, "jiri", "update")
}

func updateBoring() {
	src := filepath.Join(*boring, "src")

	infof("Updating sources...")
	run(src, "git", "fetch")
	run(src, "git", "checkout", *commit)

	infof("Generating build files...")
	run(*boring, "python", filepath.Join(src, "util", "generate_build_files.py"), "gn")

	infof("Updating Jiri manifest...")
	updateManifest("project", src, filepath.Join(*boring, "manifest"))
}

func updateRust() {
	run("", filepath.Join(*boring, "rust/boringssl-sys/bindgen.sh"))
}

// To update Zircon's uboringssl library, we update the revision number in the README file and
// copy any files present in uboringssl that do not match their counterpart in BoringSSL
func updateZircon() {
	infof("  Updating README file...")

	rev := getGitRevision(filepath.Join(*boring, "src"))
	readmePath := filepath.Join(*zircon, "README.fuchsia.md")

	info, err := os.Stat(readmePath)
	if err != nil {
		log.Fatal(err)
	}
	off := int64(0)
	hashlen := int64(len(rev))
	rev[hashlen-1] = '/'
	if hashlen < info.Size() {
		off = info.Size() - (hashlen + 1)
	}

	readme, err := os.OpenFile(readmePath, os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer readme.Close()

	if _, err = readme.WriteAt(rev, off); err != nil {
		log.Fatal(err)
	}

	infof("  Updating sources from BoringSSL...")
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
			manual_files[stem] = true
		}
		// Check for files needing manual changes based on original file's digest
		boringHash := sha256sum(boringPath)
		zirconHash, found := edited_files[stem]
		if found {
			if boringHash != zirconHash {
				manual_files[stem] = true
			}
			return nil
		}
		// Copy files that have changed
		if boringHash != sha256sum(zirconPath) {
			run(*fuchsia, "cp", boringPath, zirconPath)
		}
		return nil
	}
	if err := filepath.Walk(*zircon, walker); err != nil {
		log.Fatal(err)
	}
}

func updateGarnet() {
	infof("  Updating Jiri manifest...")
	updateManifest("import", *boring, filepath.Join(*garnet, "third_party"))
}

// Main function
func main() {
	flag.Parse()
	if len(*fuchsia) == 0 {
		log.Fatal(errors.New("FUCHSIA_DIR not set and --fuchsia not specified"))
	}
	*boring = filepath.Join(*fuchsia, *boring)
	*garnet = filepath.Join(*fuchsia, *garnet)
	*zircon = filepath.Join(*fuchsia, *zircon)

	if *reset {
		infof("Resetting Fuchsia...")
		resetRepo(*garnet)
		resetRepo(*zircon)
		resetRepo(*boring)
		resetRepo(filepath.Join(*boring, "src"))
		infof("Done!")
		return
	}

	if *submit {
		t := time.Now()
		topic := fmt.Sprintf("boringssl-roll-%04d-%02d%02d-%02d%02d",
			t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute())
		infof("Submitting topic '" + topic + "'...")
		submitTopic(*garnet, topic)
		submitTopic(*zircon, topic)
		submitTopic(*boring, topic)
		submitTopic(filepath.Join(*boring, "src"), topic)
		infof("Done!")
		return
	}

	packages := make(map[string]bool)
	tests := make(map[string]bool)
	commits := make(map[string]bool)

	if !*skipFuchsia {
		infof("Synchronizing Fuchsia...")
		updateFuchsia()
		infof("Done!")
	}

	if !*skipBoring {
		infof("Updating BoringSSL from upstream...")
		updateBoring()
		packages["garnet/packages/boringssl"] = true
		tests["/system/test/disabled/crypto_test"] = true
		tests["/system/test/ssl_test"] = true
		commits["third_party/boringssl"] = true
		infof("Done!")
	}

	if !*skipRust {
		infof("Updating Rust bindings...")
		updateRust()
		infof("Done!")
	}

	if !*skipZircon {
		infof("Updating Zircon's uboringssl library...")
		updateZircon()
		tests["/boot/test/sys/crypto_test"] = true
		commits["zircon"] = true
		infof("Done!")
	}
	// Warn about missing and edited files; these files probably need manual intervention
	if len(manual_files) != 0 {
		warnf("ERROR: These files could not be automatically resolved:")
		for file := range manual_files {
			warnf(file)
		}
		log.Fatal("Please resolve these files and try again.")
		return
	}

	if !*skipGarnet {
		infof("Committing changes and updating Garnet...")
		for commit := range commits {
			commitChanges(commit)
		}
		updateGarnet()
		commitChanges(*garnet)
		commits["garnet"] = true
		infof("Done!")
	}

	if len(packages) == 0 {
		infof("\nNow, build Zircon.")
	} else {
		infof("\nNow, do a full build with the following packages:")
		for pkg := range packages {
			infof("  " + pkg)
		}
	}

	if len(tests) != 0 {
		infof("Then, launch Fuchsia and run the following tests:")
		for test := range tests {
			infof("  " + test)
		}
	}

	if len(commits) != 0 {
		infof("If those tests pass; push the commits in:")
		for commit := range commits {
			infof("  " + commit)
		}
	}
}
