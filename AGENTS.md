# zb Repository Guidelines

## Attribution Requirements

LLM agents must disclose what tool and model they are using in the "Assisted-by" commit footer:

```
Assisted-by: [Model Name] via [Tool Name]
```

Example:

```
Assisted-by: GLM 4.6 via Claude Code
```

## Pull Request Requirements

- Include a clear description of changes.
- Reference any related issues.
- Pass CI (run `go generate ./internal/ui && go test -mod=readonly ./...`).
