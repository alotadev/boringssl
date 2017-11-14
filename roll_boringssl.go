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
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	repo = flag.String("repo", "//third_party/boringssl", "Path to repository")
)

func run(cwd string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	fmt.Printf("> cd %s && %s %s\n", cwd, name, strings.Join(args, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	fmt.Printf("%s", out)
	return string(out), nil
}

func checkoutLatest() error {
	_, err := run(filepath.Join(*repo, "src"), "git", "checkout", "origin/master")
	return err
}

func generateBuildFiles() error {
	_, err := run(*repo, "python", filepath.Join("src", "util", "generate_build_files.py"), "gn")
	return err
}

func getGitRevision() (string, error) {
	out, err := run(filepath.Join(*repo, "src"), "git", "rev-list", "HEAD", "--max-count=1")
	return strings.TrimSpace(string(out)), err
}

func updateManifest() error {
	revision, err := getGitRevision()
	if err != nil {
		return err
	}
	_, err = run(*repo, "jiri", "edit", "-project=third_party/boringssl/src="+revision, "manifest")
	return err
}

func main() {
	flag.Parse()
	if strings.HasPrefix(*repo, "//") {
		root, ok := os.LookupEnv("FUCHSIA_DIR")
		if !ok {
			log.Fatal(errors.New("FUCHSIA_DIR not set; can't locate " + *repo))
		}
		*repo = root + (*repo)[2:]
	}

	if err := checkoutLatest(); err != nil {
		log.Fatal(err)
	}
	if err := updateManifest(); err != nil {
		log.Fatal(err)
	}
	if err := generateBuildFiles(); err != nil {
		log.Fatal(err)
	}
}
