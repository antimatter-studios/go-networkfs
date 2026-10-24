### Changed

- **The module path is `github.com/antimatter-studios/go-networkfs`.** The
  repository moved from `christhomas`; importers change
  `github.com/christhomas/go-networkfs/...` to the new path. No old-path
  alias is kept.

### Security

- **go.mod requires go1.26.10**, which distrusts the compromised intermediate
  certificates in `crypto/x509` (GO-2026-6809).
