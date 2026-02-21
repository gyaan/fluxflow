## Summary

<!-- What does this PR do? One or two sentences. -->

## Motivation

<!-- Why is this change needed? Link to an issue if applicable. Closes #___  -->

## Changes

<!-- Bullet list of the concrete changes made. -->

-
-

## Type of change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that would cause existing behaviour to change)
- [ ] Refactor / performance improvement (no functional change)
- [ ] Documentation update

## Testing

<!-- How was this tested? What new tests were added? -->

- [ ] Unit tests added / updated (`go test ./...` passes)
- [ ] Manual end-to-end test performed (describe below)
- [ ] Race detector clean (`go test -race ./...`)

**Manual test steps:**
```bash
# Example:
curl -X POST http://localhost:8080/v1/events ...
```

## Checklist

- [ ] Code follows the existing style (no unnecessary abstractions, no unused exports)
- [ ] `go vet ./...` produces no warnings
- [ ] `CGO_ENABLED=0 go build ./...` succeeds
- [ ] Relevant documentation (README, DEEPDIVE, TEST) has been updated
- [ ] No new external dependencies added without discussion

## Screenshots / output

<!-- If applicable, paste the before/after server response or benchmark numbers. -->
