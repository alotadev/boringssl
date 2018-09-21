#!/bin/bash

# Copyright 2018 The Fuchsia Authors. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

set -e

# cd to the directory this script lives in
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Construct a header file which imports every BoringSSL header.
for header in $(ls ../../src/include/openssl/); do
    # Skip certain headers which contain platform-specific logic, and will not
    # compile on all platforms.
    if [[ "$header" != "arm_arch.h" && \
          "$header" != "lhash_macros.h" ]]; then
        echo "#include <openssl/${header}>" >> bindgen.h
    fi
done

# TODO(joshlf):
# - Use the --use-core flag once std isn't required (see
#   https://github.com/rust-lang-nursery/rust-bindgen/issues/1015)

# Whitelist BoringSSL-related symbols so we don't get non-BoringSSL symbols such
# as platform-specific symbols (specific to the platform that is running
# 'bindgen') and other C standard library symbols.
WHITELIST="(ERR|BIO|CRYPTO|RAND|V_ASN1|ASN1|B_ASN1|CBS_ASN1|CAST|EVP|CBS|CBB|CIPHER|OPENSSL|SSLEAY|DH|DES|DIGEST|DSA|NID|EC|ECDSA|ECDH|ED25519|X25519|PKCS5_PBKDF2|SHA|SHA1|SHA224|SHA256|SHA384|SHA512|HMAC)_.*"
# NOTE(joshlf) on --target: Currently, we just pass x86_64 since none of the
# symbols we're linking against are architecture-specific (they may be
# word-size-specific, but Fuchsia only targets 64-bit platforms). If this ever
# becomes a problem, then the thing to do is probably to generate different
# files for different platforms (bindgen_x86_64.rs, bindgen_arm64.rs, etc) and
# conditionally compile them depending on target.
bindgen bindgen.h --whitelist-function "$WHITELIST" --whitelist-type "$WHITELIST" \
    --whitelist-var "$WHITELIST" -o src/lib.rs -- -I ../../src/include --target=x86_64-fuchsia

TMP="$(mktemp)"

# Prepend copyright comment, #[allow] for various warnings we don't care about,
# and a line telling Rust to link against libcrypto.
cat >> "$TMP" <<EOF
// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

#![allow(non_camel_case_types)]
#![allow(non_snake_case)]
#![allow(non_upper_case_globals)]

#[link(name = "crypto")] extern {}

EOF

cat src/lib.rs >> "$TMP"
mv "$TMP" src/lib.rs

rm bindgen.h
