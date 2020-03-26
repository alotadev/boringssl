Name: boringssl
URL: https://boringssl.googlesource.com/boringssl
Version: git
License: BSDish
License File: src/LICENSE
Security Critical: yes

Description:
A fork of OpenSSL, as described at https://www.imperialviolet.org/2014/06/20/boringssl.html

Prerequisites:
* apt-get install curl git golang perl python
* curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
* cargo install bindgen

To update:
1. `rustup update`
1. `go run roll_boringssl.go`
1. Test according to instructions given by the previous step.
1. Commit, review, and submit the changes to this repository.
1. Update the **internal** //integration/third_party/flower manifest with merged revision.

Upstream revision:
https://fuchsia.googlesource.com/third_party/boringssl/+/9ae40ce9ad7e2b1e0140dc98318bc742e8387f88/
