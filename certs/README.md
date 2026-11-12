# certs/

Optional extra CA certificates (`*.crt`) trusted **only by the build stage** of the
Dockerfile, so `go mod download` works behind a TLS-inspecting corporate proxy
that re-signs HTTPS traffic. The runtime image never receives them.

Everything here except this README and `.gitkeep` is gitignored: a corporate CA
does not belong in the repository. Outside such a network leave this folder empty.

On macOS, `make corp-ca` exports the CA from your keychain. It only reads a
public certificate; no private key is involved. Set `CORP_CA_NAME` to match your
proxy's CA common name.
