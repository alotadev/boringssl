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
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const BundleURL = "https://hg.mozilla.org/mozilla-central/raw-file/tip/security/nss/lib/ckfw/builtins/certdata.txt"

var (
	fuchsia = flag.String("fuchsia", os.Getenv("FUCHSIA_DIR"), "Fuchsia root directory")
	boring  = flag.String("boring", "third_party/boringssl", "Path to repository")
	commit  = flag.String("commit", "origin/upstream/master", "Upstream commit-ish to check out")
	bundle  = flag.String("bundle", BundleURL, "URL to retrieve certificates from")

	skipBoring = flag.Bool("skip-boring", false, "Don't update upstream sources or build files")
	skipBundle = flag.Bool("skip-bundle", false, "Don't update the root certificate bundle")
)

func run(cwd string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if len(cwd) > 0 {
		cmd.Dir = filepath.Join(*fuchsia, cwd)
		fmt.Printf("> cd %s\n", cmd.Dir)
	}
	fmt.Printf("> %s %s\n", name, strings.Join(args, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func checkoutUpstream() error {
	_, err := run(filepath.Join(*boring, "src"), "git", "fetch")
	if err != nil {
		return err
	}
	_, err = run(filepath.Join(*boring, "src"), "git", "checkout", *commit)
	if err != nil {
		return err
	}
	return nil
}

func generateBuildFiles() error {
	_, err := run(*boring, "python", filepath.Join("src", "util", "generate_build_files.py"), "gn")
	return err
}

func getGitRevision() (string, error) {
	out, err := run(filepath.Join(*boring, "src"), "git", "rev-list", "HEAD", "--max-count=1")
	return strings.TrimSpace(string(out)), err
}

func updateManifest() error {
	revision, err := getGitRevision()
	if err != nil {
		return err
	}
	_, err = run(*boring, "jiri", "edit", "-project=third_party/boringssl/src="+revision, "manifest")
	return err
}

func getCertData(url string) ([]byte, error) {
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return ioutil.ReadAll(response.Body)
}

func stampCertData(url string, certdata []byte) error {
	digest := sha256.Sum256(certdata)
	hexdigest := hex.EncodeToString(digest[:])

	stamp := "URL:    " + url + "\n"
	stamp += "SHA256: " + hexdigest + "\n"
	stamp += "Time:   " + time.Now().String() + "\n"

	stampfile, err := os.Create("certdata.stamp")
	if err != nil {
		return err
	}
	defer stampfile.Close()
	_, err = stampfile.WriteString(stamp)
	return err
}

func convertToPEM(certdata []byte) error {
	if err := ioutil.WriteFile("certdata.txt", certdata, 0644); err != nil {
		return err
	}
	out, err := run(*boring, "go", "run", "convert_mozilla_certdata.go")
	if err != nil {
		return err
	}
	return ioutil.WriteFile("certdata.pem", []byte(out), 0644)
}

func updateBoring() error {
	if *skipBoring {
		return nil
	}
	if err := checkoutUpstream(); err != nil {
		return err
	}
	if err := updateManifest(); err != nil {
		return err
	}
	if err := generateBuildFiles(); err != nil {
		return err
	}
	return nil
}

func updateBundle() error {
	if *skipBundle {
		return nil
	}
	certdata, err := getCertData(*bundle)
	if err != nil {
		return err
	}
	if err := stampCertData(*bundle, certdata); err != nil {
		return err
	}
	if err := convertToPEM(certdata); err != nil {
		return err
	}
	return nil
}

func main() {
	flag.Parse()
	if len(*fuchsia) == 0 {
		log.Fatal(errors.New("FUCHSIA_DIR not set and --fuchsia not specified"))
	}
	if err := updateBoring(); err != nil {
		log.Fatal(err)
	}
	if err := updateBundle(); err != nil {
		log.Fatal(err)
	}
}
