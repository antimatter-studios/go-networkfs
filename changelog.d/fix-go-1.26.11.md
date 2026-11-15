### Security

- **go.mod requires go1.26.11**, which fixes the `net/http` server crash from
  a `MaxBytesReader` header race (GO-2026-6859).
