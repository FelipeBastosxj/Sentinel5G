## Summary

<!-- What does this change do, and why? -->

## Checklist

- [ ] Unit tests pass for every toolchain touched
      (`go test ./pkg/... ./api/... -v -cover`, `pytest` in `cmd/ai-engine`)
- [ ] `golangci-lint run` / `black --check . && flake8 .` (as applicable) pass
- [ ] CRD manifests updated if `api/v1alpha1/*_types.go` changed
      (`config/crd/bases/*.yaml` **and** `charts/sentinel5g-operator/templates/crd.yaml`)
- [ ] eBPF changes reviewed for kernel-version compatibility and stack usage
      (`make -C bpf` compiles cleanly)
- [ ] `helm lint charts/sentinel5g-operator` passes if chart/templates changed
- [ ] Docs updated (`docs/`, `README.md`) if behavior or configuration changed

## Related issues

<!-- Closes #123 -->
